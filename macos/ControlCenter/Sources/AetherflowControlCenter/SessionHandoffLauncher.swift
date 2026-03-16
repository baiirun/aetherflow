import Foundation
import OSLog
import Darwin

struct SessionHandoffCommandOutput: Equatable, Sendable {
    let status: Int32
    let stdout: String
    let stderr: String
}

enum SessionHandoffLaunchError: LocalizedError, Equatable {
    case unavailable(String)
    case invalidMetadata(String)
    case launchFailed(String)

    var errorDescription: String? {
        switch self {
        case let .unavailable(message), let .invalidMetadata(message), let .launchFailed(message):
            return message
        }
    }
}

struct SessionHandoffLauncher: Sendable {
    typealias Runner = @Sendable (_ executable: String, _ arguments: [String], _ currentDirectory: String) throws -> SessionHandoffCommandOutput

    private static let logger = Logger(subsystem: "com.baiirun.aetherflow.controlcenter", category: "session-handoff")
    private static let commandTimeout: TimeInterval = 5

    private let runner: Runner

    init(runner: @escaping Runner = Self.runProcess) {
        self.runner = runner
    }

    func launch(session: DaemonSessionMetadataPayload, transport: TransportSnapshot) async throws {
        let validated = try Self.validatedMetadata(session: session, transport: transport)
        assert(!validated.workingDirectory.isEmpty)
        assert(!validated.cliPath.isEmpty)
        assert(!validated.sessionID.isEmpty)
        let command = Self.terminalCommand(validated: validated)
        let arguments = Self.appleScriptArguments(command: command)
        try await Self.runBlocking {
            Self.logger.info(
                "Launching Opencode handoff executable=/usr/bin/osascript cwd=\(validated.workingDirectory, privacy: .public) args=\(arguments.joined(separator: " "), privacy: .public)"
            )
            let launchOutput = try runner("/usr/bin/osascript", arguments, validated.workingDirectory)
            guard launchOutput.status == 0 else {
                Self.logger.error(
                    "Opencode handoff failed status=\(launchOutput.status) stdout=\(launchOutput.stdout, privacy: .public) stderr=\(launchOutput.stderr, privacy: .public)"
                )
                throw SessionHandoffLaunchError.launchFailed(Self.commandFailureMessage(launchOutput))
            }
            Self.logger.info("Opencode handoff launched successfully for session \(validated.sessionID, privacy: .public)")
        }
    }

    static func terminalCommand(
        session: DaemonSessionMetadataPayload,
        transport: TransportSnapshot
    ) throws -> String {
        terminalCommand(validated: try validatedMetadata(session: session, transport: transport))
    }

    private static func terminalCommand(
        validated: (workingDirectory: String, cliPath: String, sessionID: String, serverRef: String)
    ) -> String {
        let command = [
            "cd \(shellQuoted(validated.workingDirectory))",
            "\(shellCommandExecutable(validated.cliPath)) session attach \(shellQuoted(validated.sessionID)) --server \(shellQuoted(validated.serverRef))"
        ].joined(separator: " && ")
        return command
    }

    static func appleScriptArguments(command: String) -> [String] {
        [
            "-e", "tell application \"Terminal\"",
            "-e", "activate",
            "-e", "do script \(appleScriptQuoted(command))",
            "-e", "end tell"
        ]
    }

    private static func shellCommandExecutable(_ cliPath: String) -> String {
        if cliPath == "af" {
            return cliPath
        }
        return shellQuoted(cliPath)
    }

    private static func shellQuoted(_ value: String) -> String {
        "'\(value.replacingOccurrences(of: "'", with: "'\"'\"'"))'"
    }

    private static func appleScriptQuoted(_ value: String) -> String {
        let escaped = value
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
        return "\"\(escaped)\""
    }

    private static func runBlocking<T: Sendable>(
        _ work: @escaping @Sendable () throws -> T
    ) async throws -> T {
        try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    continuation.resume(returning: try work())
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    private static func runProcess(
        executable: String,
        arguments: [String],
        currentDirectory: String
    ) throws -> SessionHandoffCommandOutput {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        process.currentDirectoryURL = URL(fileURLWithPath: currentDirectory)

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            throw SessionHandoffLaunchError.launchFailed(
                "Failed to launch the handoff command: \(error.localizedDescription)"
            )
        }

        if waitForExit(process, timeout: commandTimeout) {
            logger.error(
                "Opencode handoff command timed out executable=\(executable, privacy: .public) cwd=\(currentDirectory, privacy: .public) args=\(arguments.joined(separator: " "), privacy: .public)"
            )
            terminate(process: process)
            throw SessionHandoffLaunchError.launchFailed(
                "Opencode handoff timed out after \(Int(commandTimeout)) seconds."
            )
        }

        return SessionHandoffCommandOutput(
            status: process.terminationStatus,
            stdout: String(decoding: stdoutPipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
                .trimmingCharacters(in: .whitespacesAndNewlines),
            stderr: String(decoding: stderrPipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
                .trimmingCharacters(in: .whitespacesAndNewlines)
        )
    }

    private static func validatedMetadata(
        session: DaemonSessionMetadataPayload,
        transport: TransportSnapshot
    ) throws -> (workingDirectory: String, cliPath: String, sessionID: String, serverRef: String) {
        guard session.attachable else {
            throw SessionHandoffLaunchError.unavailable(
                "Opencode handoff is unavailable for this session. The daemon has not exposed an attachable route yet."
            )
        }

        guard let workingDirectory = transport.workingDirectory.nonEmptyTrimmed else {
            throw SessionHandoffLaunchError.invalidMetadata(
                "Cannot launch Opencode handoff because the app has no working directory context."
            )
        }
        guard let cliPath = transport.cliPath.nonEmptyTrimmed else {
            throw SessionHandoffLaunchError.invalidMetadata(
                "Cannot launch Opencode handoff because the CLI path is missing."
            )
        }
        guard let sessionID = session.sessionID.nonEmptyTrimmed else {
            throw SessionHandoffLaunchError.invalidMetadata(
                "Cannot launch Opencode handoff because session_id is missing from the daemon metadata."
            )
        }
        guard let serverRef = session.serverRef.nonEmptyTrimmed else {
            throw SessionHandoffLaunchError.invalidMetadata(
                "Cannot launch Opencode handoff because server_ref is missing from the daemon metadata."
            )
        }

        var isDirectory = ObjCBool(false)
        guard FileManager.default.fileExists(atPath: workingDirectory, isDirectory: &isDirectory), isDirectory.boolValue else {
            throw SessionHandoffLaunchError.invalidMetadata(
                "Cannot launch Opencode handoff because the working directory is unavailable."
            )
        }

        return (workingDirectory, cliPath, sessionID, serverRef)
    }

    private static func commandFailureMessage(_ output: SessionHandoffCommandOutput) -> String {
        if !output.stderr.isEmpty {
            return output.stderr
        }
        if !output.stdout.isEmpty {
            return output.stdout
        }
        return "Command exited with status \(output.status)."
    }

    private static func waitForExit(_ process: Process, timeout: TimeInterval) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while process.isRunning {
            if Date() >= deadline {
                return true
            }
            Thread.sleep(forTimeInterval: 0.05)
        }
        return false
    }

    private static func terminate(process: Process) {
        guard process.isRunning else {
            return
        }

        process.terminate()
        Thread.sleep(forTimeInterval: 0.1)
        if process.isRunning {
            kill(process.processIdentifier, SIGKILL)
        }
        process.waitUntilExit()
    }
}

private extension String {
    var nonEmptyTrimmed: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
