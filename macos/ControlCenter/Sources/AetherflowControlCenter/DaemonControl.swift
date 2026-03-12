import Foundation

enum DaemonControlError: LocalizedError {
    case executableNotFound(String)
    case connectionFailed(String)
    case invalidResponse(String)
    case commandFailed(String)

    var errorDescription: String? {
        switch self {
        case .executableNotFound(let detail),
             .connectionFailed(let detail),
             .invalidResponse(let detail),
             .commandFailed(let detail):
            return detail
        }
    }
}

protocol DaemonControlling: Sendable {
    func fetchLifecycle(daemonURL: String) async throws -> DaemonLifecyclePayload
    func requestStop(daemonURL: String, force: Bool) async throws -> DaemonStopResponse
    func requestStart(context: ShellBootstrapContext) async throws -> DaemonStartReceipt
}

struct DaemonLifecyclePayload: Codable, Equatable, Sendable {
    let state: String
    let daemonURL: String
    let project: String
    let serverURL: String
    let spawnPolicy: String
    let activeSessionCount: Int
    let activeSessionIDs: [String]
    let lastError: String
    let updatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case state
        case daemonURL = "daemon_url"
        case project
        case serverURL = "server_url"
        case spawnPolicy = "spawn_policy"
        case activeSessionCount = "active_session_count"
        case activeSessionIDs = "active_session_ids"
        case lastError = "last_error"
        case updatedAt = "updated_at"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        state = try container.decode(String.self, forKey: .state)
        daemonURL = try container.decodeIfPresent(String.self, forKey: .daemonURL) ?? ""
        project = try container.decodeIfPresent(String.self, forKey: .project) ?? ""
        serverURL = try container.decodeIfPresent(String.self, forKey: .serverURL) ?? ""
        spawnPolicy = try container.decodeIfPresent(String.self, forKey: .spawnPolicy) ?? ""
        activeSessionCount = try container.decodeIfPresent(Int.self, forKey: .activeSessionCount) ?? 0
        activeSessionIDs = try container.decodeIfPresent([String].self, forKey: .activeSessionIDs) ?? []
        lastError = try container.decodeIfPresent(String.self, forKey: .lastError) ?? ""
        updatedAt = try Self.decodeDate(container: container, key: .updatedAt)
    }

    init(
        state: String,
        daemonURL: String,
        project: String,
        serverURL: String,
        spawnPolicy: String,
        activeSessionCount: Int,
        activeSessionIDs: [String],
        lastError: String,
        updatedAt: Date
    ) {
        self.state = state
        self.daemonURL = daemonURL
        self.project = project
        self.serverURL = serverURL
        self.spawnPolicy = spawnPolicy
        self.activeSessionCount = activeSessionCount
        self.activeSessionIDs = activeSessionIDs
        self.lastError = lastError
        self.updatedAt = updatedAt
    }

    private static func decodeDate(
        container: KeyedDecodingContainer<CodingKeys>,
        key: CodingKeys
    ) throws -> Date {
        guard let rawValue = try container.decodeIfPresent(String.self, forKey: key) else {
            return .now
        }
        if let parsed = Self.parseDate(rawValue) {
            return parsed
        }
        throw DecodingError.dataCorruptedError(forKey: key, in: container, debugDescription: "invalid date \(rawValue)")
    }

    private static func parseDate(_ value: String) -> Date? {
        let fractionalFormatter = ISO8601DateFormatter()
        fractionalFormatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let parsed = fractionalFormatter.date(from: value) {
            return parsed
        }

        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: value)
    }
}

struct DaemonStopResponse: Codable, Equatable, Sendable {
    let outcome: String
    let status: DaemonLifecyclePayload
    let message: String

    init(outcome: String, status: DaemonLifecyclePayload, message: String) {
        self.outcome = outcome
        self.status = status
        self.message = message
    }
}

struct DaemonStartReceipt: Equatable, Sendable {
    let message: String
}

private struct RPCEnvelope<Result: Decodable>: Decodable {
    let success: Bool
    let result: Result?
    let error: String?
}

private struct CommandOutput: Sendable {
    let status: Int32
    let stdout: String
    let stderr: String
}

struct DefaultDaemonController: DaemonControlling, Sendable {
    private let session: URLSession

    init(session: URLSession = .shared) {
        self.session = session
    }

    func fetchLifecycle(daemonURL: String) async throws -> DaemonLifecyclePayload {
        guard let url = URL(string: daemonURL + "/api/v1/lifecycle") else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        let (data, response) = try await session.data(from: url)
        return try decodeEnvelope(data, response: response)
    }

    func requestStop(daemonURL: String, force: Bool) async throws -> DaemonStopResponse {
        let urlString = force ? daemonURL + "/api/v1/shutdown?force=true" : daemonURL + "/api/v1/shutdown"
        guard let url = URL(string: urlString) else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        let (data, response) = try await session.data(for: request)
        return try decodeEnvelope(data, response: response)
    }

    func requestStart(context: ShellBootstrapContext) async throws -> DaemonStartReceipt {
        let output = try await Self.runBlocking {
            try Self.runCommand(
                executable: context.cliPath,
                arguments: ["daemon", "start", "--detach", "--project", context.projectName],
                currentDirectory: context.workingDirectory
            )
        }
        guard output.status == 0 else {
            throw DaemonControlError.commandFailed(Self.commandFailureMessage(output))
        }
        let message = output.stdout.nonEmptyTrimmed ?? "daemon start requested"
        return DaemonStartReceipt(message: message)
    }

    private func decodeEnvelope<T: Decodable>(_ data: Data, response: URLResponse) throws -> T {
        guard let httpResponse = response as? HTTPURLResponse else {
            throw DaemonControlError.invalidResponse("unexpected non-HTTP response")
        }
        if httpResponse.statusCode >= 500 {
            throw DaemonControlError.connectionFailed("daemon returned server error \(httpResponse.statusCode)")
        }
        let envelope = try JSONDecoder().decode(RPCEnvelope<T>.self, from: data)
        guard envelope.success, let result = envelope.result else {
            throw DaemonControlError.invalidResponse(envelope.error?.nonEmptyTrimmed ?? "request failed without error message")
        }
        return result
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

    private static func runCommand(
        executable: String,
        arguments: [String],
        currentDirectory: String
    ) throws -> CommandOutput {
        guard FileManager.default.isExecutableFile(atPath: executable) || executable == "af" else {
            throw DaemonControlError.executableNotFound("af executable not found at \(executable)")
        }

        let process = Process()
        if executable == "af" {
            process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
            process.arguments = [executable] + arguments
        } else {
            process.executableURL = URL(fileURLWithPath: executable)
            process.arguments = arguments
        }
        process.currentDirectoryURL = URL(fileURLWithPath: currentDirectory)

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            throw DaemonControlError.commandFailed("failed to launch \(executable): \(error.localizedDescription)")
        }

        process.waitUntilExit()

        let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
        return CommandOutput(
            status: process.terminationStatus,
            stdout: String(decoding: stdoutData, as: UTF8.self).trimmed,
            stderr: String(decoding: stderrData, as: UTF8.self).trimmed
        )
    }

    private static func commandFailureMessage(_ output: CommandOutput) -> String {
        if let stderr = output.stderr.nonEmptyTrimmed {
            return stderr
        }
        if let stdout = output.stdout.nonEmptyTrimmed {
            return stdout
        }
        return "daemon command failed with exit code \(output.status)"
    }
}

private extension String {
    var trimmed: String {
        trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var nonEmptyTrimmed: String? {
        let value = trimmed
        return value.isEmpty ? nil : value
    }
}
