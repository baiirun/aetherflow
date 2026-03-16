import Foundation

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

    private let runner: Runner

    init(runner: @escaping Runner = Self.runProcess) {
        self.runner = runner
    }

    func launch(session: DaemonSessionMetadataPayload, transport: TransportSnapshot) async throws {
        let validated = try Self.validatedMetadata(session: session, transport: transport)
        let validationInvocation = Self.validationInvocation(
            cliPath: validated.cliPath,
            serverRef: validated.serverRef
        )
        let command = try Self.terminalCommand(session: session, transport: transport)
        let arguments = Self.appleScriptArguments(command: command)
        try await Self.runBlocking {
            let validationOutput = try runner(
                validationInvocation.executable,
                validationInvocation.arguments,
                validated.workingDirectory
            )
            guard validationOutput.status == 0 else {
                throw SessionHandoffLaunchError.launchFailed(Self.commandFailureMessage(validationOutput))
            }
            try Self.validateSessionRecord(
                sessionID: validated.sessionID,
                serverRef: validated.serverRef,
                output: validationOutput
            )

            let launchOutput = try runner("/usr/bin/osascript", arguments, validated.workingDirectory)
            guard launchOutput.status == 0 else {
                throw SessionHandoffLaunchError.launchFailed(Self.commandFailureMessage(launchOutput))
            }
        }
    }

    static func terminalCommand(
        session: DaemonSessionMetadataPayload,
        transport: TransportSnapshot
    ) throws -> String {
        let validated = try validatedMetadata(session: session, transport: transport)

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

    private static func validationInvocation(
        cliPath: String,
        serverRef: String
    ) -> (executable: String, arguments: [String]) {
        let arguments = ["sessions", "--json", "--server", serverRef]
        if cliPath == "af" {
            return ("/usr/bin/env", ["af"] + arguments)
        }
        return (cliPath, arguments)
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

        process.waitUntilExit()
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
        return (workingDirectory, cliPath, sessionID, serverRef)
    }

    private static func validateSessionRecord(
        sessionID: String,
        serverRef: String,
        output: SessionHandoffCommandOutput
    ) throws {
        struct SessionRecord: Decodable {
            let sessionID: String

            private enum CodingKeys: String, CodingKey {
                case sessionID = "session_id"
            }
        }

        let data = Data(output.stdout.utf8)
        let records: [SessionRecord]
        do {
            records = try JSONDecoder().decode([SessionRecord].self, from: data)
        } catch {
            throw SessionHandoffLaunchError.launchFailed(
                "Unable to validate the Opencode handoff target from `af sessions --json`: \(error.localizedDescription)"
            )
        }

        guard records.contains(where: { $0.sessionID == sessionID }) else {
            throw SessionHandoffLaunchError.launchFailed(
                "Session \(sessionID) is not available in the local session registry for \(serverRef)."
            )
        }
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
}

private extension String {
    var nonEmptyTrimmed: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
