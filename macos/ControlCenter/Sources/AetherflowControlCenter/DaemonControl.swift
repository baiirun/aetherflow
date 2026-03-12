import Darwin
import Foundation

private let daemonControlQueue = DispatchQueue(label: "com.baiirun.aetherflow.control-center.daemon-control", qos: .userInitiated)

enum DaemonControlError: LocalizedError {
    case executableNotFound(String)
    case socketPathTooLong(String)
    case connectionFailed(String)
    case writeFailed(String)
    case invalidResponse(String)
    case commandFailed(String)

    var errorDescription: String? {
        switch self {
        case .executableNotFound(let detail),
             .socketPathTooLong(let detail),
             .connectionFailed(let detail),
             .writeFailed(let detail),
             .invalidResponse(let detail),
             .commandFailed(let detail):
            return detail
        }
    }
}

protocol DaemonControlling: Sendable {
    func fetchLifecycle(socketPath: String) async throws -> DaemonLifecyclePayload
    func requestStop(socketPath: String, force: Bool) async throws -> DaemonStopResponse
    func requestStart(context: ShellBootstrapContext) async throws -> DaemonStartReceipt
}

struct DaemonLifecyclePayload: Codable, Equatable, Sendable {
    let state: String
    let socketPath: String
    let project: String
    let serverURL: String
    let spawnPolicy: String
    let activeSessionCount: Int
    let activeSessionIDs: [String]
    let lastError: String
    let updatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case state
        case socketPath = "socket_path"
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
        socketPath = try container.decodeIfPresent(String.self, forKey: .socketPath) ?? ""
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
        socketPath: String,
        project: String,
        serverURL: String,
        spawnPolicy: String,
        activeSessionCount: Int,
        activeSessionIDs: [String],
        lastError: String,
        updatedAt: Date
    ) {
        self.state = state
        self.socketPath = socketPath
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

private struct RPCRequest<Params: Encodable>: Encodable {
    let method: String
    let params: Params
}

private struct EmptyParams: Encodable {}

private struct StopParams: Encodable {
    let force: Bool
}

private struct CommandOutput: Sendable {
    let status: Int32
    let stdout: String
    let stderr: String
}

struct DefaultDaemonController: DaemonControlling, Sendable {
    func fetchLifecycle(socketPath: String) async throws -> DaemonLifecyclePayload {
        try await Self.runBlocking {
            try Self.rpc(socketPath: socketPath, method: "daemon.lifecycle", params: EmptyParams())
        }
    }

    func requestStop(socketPath: String, force: Bool) async throws -> DaemonStopResponse {
        try await Self.runBlocking {
            try Self.rpc(socketPath: socketPath, method: "daemon.stop", params: StopParams(force: force))
        }
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

    private static func runBlocking<T: Sendable>(
        _ work: @escaping @Sendable () throws -> T
    ) async throws -> T {
        try await withCheckedThrowingContinuation { continuation in
            daemonControlQueue.async {
                do {
                    continuation.resume(returning: try work())
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    private static func rpc<Params: Encodable & Sendable, Result: Decodable & Sendable>(
        socketPath: String,
        method: String,
        params: Params
    ) throws -> Result {
        var socketAddress = sockaddr_un()
        socketAddress.sun_family = sa_family_t(AF_UNIX)

        let socketBytes = Array(socketPath.utf8CString)
        guard socketBytes.count <= MemoryLayout.size(ofValue: socketAddress.sun_path) else {
            throw DaemonControlError.socketPathTooLong("socket path is too long: \(socketPath)")
        }

        let pathOffset = MemoryLayout.offset(of: \sockaddr_un.sun_path) ?? 0
        let addressLength = socklen_t(pathOffset + socketBytes.count)
#if os(macOS)
        socketAddress.sun_len = __uint8_t(addressLength)
#endif
        withUnsafeMutablePointer(to: &socketAddress) { pointer in
            let rawPointer = UnsafeMutableRawPointer(pointer).advanced(by: pathOffset)
            socketBytes.withUnsafeBytes { sourceBytes in
                rawPointer.copyMemory(from: sourceBytes.baseAddress!, byteCount: socketBytes.count)
            }
        }

        let descriptor = socket(AF_UNIX, SOCK_STREAM, 0)
        guard descriptor >= 0 else {
            throw DaemonControlError.connectionFailed(Self.posixMessage(prefix: "failed to open unix socket"))
        }
        defer { close(descriptor) }

        let connectResult = withUnsafePointer(to: &socketAddress) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(descriptor, $0, addressLength)
            }
        }
        guard connectResult == 0 else {
            throw DaemonControlError.connectionFailed(Self.posixMessage(prefix: "failed to connect to daemon at \(socketPath)"))
        }

        var requestData = try JSONEncoder().encode(RPCRequest(method: method, params: params))
        requestData.append(0x0A)
        let writeStatus = try requestData.withUnsafeBytes { rawBuffer in
            guard let baseAddress = rawBuffer.baseAddress else {
                throw DaemonControlError.writeFailed("failed to encode daemon RPC request")
            }
            return write(descriptor, baseAddress, rawBuffer.count)
        }
        guard writeStatus >= 0 else {
            throw DaemonControlError.writeFailed(Self.posixMessage(prefix: "failed to write daemon RPC request"))
        }

        var response = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while true {
            let readCount = read(descriptor, &buffer, buffer.count)
            if readCount < 0 {
                throw DaemonControlError.connectionFailed(Self.posixMessage(prefix: "failed to read daemon RPC response"))
            }
            if readCount == 0 {
                break
            }
            response.append(buffer, count: readCount)
        }

        let envelope = try JSONDecoder().decode(RPCEnvelope<Result>.self, from: response)
        guard envelope.success else {
            throw DaemonControlError.invalidResponse(envelope.error?.nonEmptyTrimmed ?? "daemon RPC failed without an error message")
        }
        guard let result = envelope.result else {
            throw DaemonControlError.invalidResponse("daemon RPC returned no result payload")
        }
        return result
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

    private static func posixMessage(prefix: String) -> String {
        let message = String(cString: strerror(errno))
        return "\(prefix): \(message)"
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
