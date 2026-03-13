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
    func fetchStatus(daemonURL: String) async throws -> DaemonStatusPayload
    func fetchAgentDetail(daemonURL: String, agentName: String, limit: Int) async throws -> DaemonAgentDetailPayload
    func fetchEvents(daemonURL: String, agentName: String, afterTimestamp: Int64) async throws -> DaemonEventsPayload
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
        updatedAt = try DaemonDateCoding.decodeDate(container: container, key: .updatedAt)
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

struct DaemonStatusPayload: Codable, Equatable, Sendable {
    let poolSize: Int
    let poolMode: String
    let project: String
    let spawnPolicy: String
    let agents: [DaemonAgentStatusPayload]
    let spawns: [DaemonSpawnStatusPayload]
    let queue: [DaemonQueueTaskPayload]
    let errors: [String]

    private enum CodingKeys: String, CodingKey {
        case poolSize = "pool_size"
        case poolMode = "pool_mode"
        case project
        case spawnPolicy = "spawn_policy"
        case agents
        case spawns
        case queue
        case errors
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        poolSize = try container.decodeIfPresent(Int.self, forKey: .poolSize) ?? 0
        poolMode = try container.decodeIfPresent(String.self, forKey: .poolMode) ?? ""
        project = try container.decodeIfPresent(String.self, forKey: .project) ?? ""
        spawnPolicy = try container.decodeIfPresent(String.self, forKey: .spawnPolicy) ?? ""
        agents = try container.decodeIfPresent([DaemonAgentStatusPayload].self, forKey: .agents) ?? []
        spawns = try container.decodeIfPresent([DaemonSpawnStatusPayload].self, forKey: .spawns) ?? []
        queue = try container.decodeIfPresent([DaemonQueueTaskPayload].self, forKey: .queue) ?? []
        errors = try container.decodeIfPresent([String].self, forKey: .errors) ?? []
    }

    init(
        poolSize: Int,
        poolMode: String,
        project: String,
        spawnPolicy: String,
        agents: [DaemonAgentStatusPayload],
        spawns: [DaemonSpawnStatusPayload],
        queue: [DaemonQueueTaskPayload],
        errors: [String]
    ) {
        self.poolSize = poolSize
        self.poolMode = poolMode
        self.project = project
        self.spawnPolicy = spawnPolicy
        self.agents = agents
        self.spawns = spawns
        self.queue = queue
        self.errors = errors
    }
}

struct DaemonAgentStatusPayload: Codable, Equatable, Sendable {
    let id: String
    let taskID: String
    let role: String
    let pid: Int
    let spawnTime: Date
    let taskTitle: String
    let lastLog: String
    let sessionID: String
    let state: String
    let lifecycleState: String
    let lastActivityAt: Date?
    let attentionNeeded: Bool

    private enum CodingKeys: String, CodingKey {
        case id
        case taskID = "task_id"
        case role
        case pid
        case spawnTime = "spawn_time"
        case taskTitle = "task_title"
        case lastLog = "last_log"
        case sessionID = "session_id"
        case state
        case lifecycleState = "lifecycle_state"
        case lastActivityAt = "last_activity_at"
        case attentionNeeded = "attention_needed"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(String.self, forKey: .id) ?? ""
        taskID = try container.decodeIfPresent(String.self, forKey: .taskID) ?? ""
        role = try container.decodeIfPresent(String.self, forKey: .role) ?? ""
        pid = try container.decodeIfPresent(Int.self, forKey: .pid) ?? 0
        spawnTime = try DaemonDateCoding.decodeDate(container: container, key: .spawnTime)
        taskTitle = try container.decodeIfPresent(String.self, forKey: .taskTitle) ?? ""
        lastLog = try container.decodeIfPresent(String.self, forKey: .lastLog) ?? ""
        sessionID = try container.decodeIfPresent(String.self, forKey: .sessionID) ?? ""
        state = try container.decodeIfPresent(String.self, forKey: .state) ?? ""
        lifecycleState = try container.decodeIfPresent(String.self, forKey: .lifecycleState) ?? ""
        lastActivityAt = try DaemonDateCoding.decodeOptionalDate(container: container, key: .lastActivityAt)
        attentionNeeded = try container.decodeIfPresent(Bool.self, forKey: .attentionNeeded) ?? false
    }

    init(
        id: String,
        taskID: String,
        role: String,
        pid: Int,
        spawnTime: Date,
        taskTitle: String,
        lastLog: String,
        sessionID: String,
        state: String,
        lifecycleState: String,
        lastActivityAt: Date?,
        attentionNeeded: Bool
    ) {
        self.id = id
        self.taskID = taskID
        self.role = role
        self.pid = pid
        self.spawnTime = spawnTime
        self.taskTitle = taskTitle
        self.lastLog = lastLog
        self.sessionID = sessionID
        self.state = state
        self.lifecycleState = lifecycleState
        self.lastActivityAt = lastActivityAt
        self.attentionNeeded = attentionNeeded
    }
}

struct DaemonSpawnStatusPayload: Codable, Equatable, Sendable {
    let spawnID: String
    let pid: Int
    let sessionID: String
    let state: String
    let lifecycleState: String
    let lastActivityAt: Date?
    let attentionNeeded: Bool
    let prompt: String
    let spawnTime: Date
    let exitedAt: Date?

    private enum CodingKeys: String, CodingKey {
        case spawnID = "spawn_id"
        case pid
        case sessionID = "session_id"
        case state
        case lifecycleState = "lifecycle_state"
        case lastActivityAt = "last_activity_at"
        case attentionNeeded = "attention_needed"
        case prompt
        case spawnTime = "spawn_time"
        case exitedAt = "exited_at"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        spawnID = try container.decodeIfPresent(String.self, forKey: .spawnID) ?? ""
        pid = try container.decodeIfPresent(Int.self, forKey: .pid) ?? 0
        sessionID = try container.decodeIfPresent(String.self, forKey: .sessionID) ?? ""
        state = try container.decodeIfPresent(String.self, forKey: .state) ?? ""
        lifecycleState = try container.decodeIfPresent(String.self, forKey: .lifecycleState) ?? ""
        lastActivityAt = try DaemonDateCoding.decodeOptionalDate(container: container, key: .lastActivityAt)
        attentionNeeded = try container.decodeIfPresent(Bool.self, forKey: .attentionNeeded) ?? false
        prompt = try container.decodeIfPresent(String.self, forKey: .prompt) ?? ""
        spawnTime = try DaemonDateCoding.decodeDate(container: container, key: .spawnTime)
        exitedAt = try DaemonDateCoding.decodeOptionalDate(container: container, key: .exitedAt)
    }

    init(
        spawnID: String,
        pid: Int,
        sessionID: String,
        state: String,
        lifecycleState: String,
        lastActivityAt: Date?,
        attentionNeeded: Bool,
        prompt: String,
        spawnTime: Date,
        exitedAt: Date?
    ) {
        self.spawnID = spawnID
        self.pid = pid
        self.sessionID = sessionID
        self.state = state
        self.lifecycleState = lifecycleState
        self.lastActivityAt = lastActivityAt
        self.attentionNeeded = attentionNeeded
        self.prompt = prompt
        self.spawnTime = spawnTime
        self.exitedAt = exitedAt
    }
}

struct DaemonQueueTaskPayload: Codable, Equatable, Sendable {
    let id: String
    let priority: Int
    let title: String
}

struct DaemonAgentDetailPayload: Decodable, Equatable, Sendable {
    let agent: DaemonAgentStatusPayload
    let session: DaemonSessionMetadataPayload
    let toolCalls: [DaemonToolCallPayload]
    let errors: [String]

    private enum CodingKeys: String, CodingKey {
        case id
        case taskID = "task_id"
        case role
        case pid
        case spawnTime = "spawn_time"
        case taskTitle = "task_title"
        case lastLog = "last_log"
        case sessionID = "session_id"
        case state
        case lifecycleState = "lifecycle_state"
        case lastActivityAt = "last_activity_at"
        case attentionNeeded = "attention_needed"
        case session
        case toolCalls = "tool_calls"
        case errors
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        agent = try DaemonAgentStatusPayload(
            id: container.decodeIfPresent(String.self, forKey: .id) ?? "",
            taskID: container.decodeIfPresent(String.self, forKey: .taskID) ?? "",
            role: container.decodeIfPresent(String.self, forKey: .role) ?? "",
            pid: container.decodeIfPresent(Int.self, forKey: .pid) ?? 0,
            spawnTime: try DaemonDateCoding.decodeDate(container: container, key: .spawnTime),
            taskTitle: container.decodeIfPresent(String.self, forKey: .taskTitle) ?? "",
            lastLog: container.decodeIfPresent(String.self, forKey: .lastLog) ?? "",
            sessionID: container.decodeIfPresent(String.self, forKey: .sessionID) ?? "",
            state: container.decodeIfPresent(String.self, forKey: .state) ?? "",
            lifecycleState: container.decodeIfPresent(String.self, forKey: .lifecycleState) ?? "",
            lastActivityAt: try DaemonDateCoding.decodeOptionalDate(container: container, key: .lastActivityAt),
            attentionNeeded: container.decodeIfPresent(Bool.self, forKey: .attentionNeeded) ?? false
        )
        session = try container.decodeIfPresent(DaemonSessionMetadataPayload.self, forKey: .session) ?? .empty
        toolCalls = try container.decodeIfPresent([DaemonToolCallPayload].self, forKey: .toolCalls) ?? []
        errors = try container.decodeIfPresent([String].self, forKey: .errors) ?? []
    }

    init(
        agent: DaemonAgentStatusPayload,
        session: DaemonSessionMetadataPayload,
        toolCalls: [DaemonToolCallPayload],
        errors: [String]
    ) {
        self.agent = agent
        self.session = session
        self.toolCalls = toolCalls
        self.errors = errors
    }
}

struct DaemonSessionMetadataPayload: Codable, Equatable, Sendable {
    let serverRef: String
    let sessionID: String
    let directory: String
    let project: String
    let originType: String
    let workRef: String
    let agentID: String
    let status: String
    let createdAt: Date?
    let lastSeenAt: Date?
    let updatedAt: Date?
    let attachable: Bool

    static let empty = DaemonSessionMetadataPayload(
        serverRef: "",
        sessionID: "",
        directory: "",
        project: "",
        originType: "",
        workRef: "",
        agentID: "",
        status: "",
        createdAt: nil,
        lastSeenAt: nil,
        updatedAt: nil,
        attachable: false
    )

    private enum CodingKeys: String, CodingKey {
        case serverRef = "server_ref"
        case sessionID = "session_id"
        case directory
        case project
        case originType = "origin_type"
        case workRef = "work_ref"
        case agentID = "agent_id"
        case status
        case createdAt = "created_at"
        case lastSeenAt = "last_seen_at"
        case updatedAt = "updated_at"
        case attachable
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        serverRef = try container.decodeIfPresent(String.self, forKey: .serverRef) ?? ""
        sessionID = try container.decodeIfPresent(String.self, forKey: .sessionID) ?? ""
        directory = try container.decodeIfPresent(String.self, forKey: .directory) ?? ""
        project = try container.decodeIfPresent(String.self, forKey: .project) ?? ""
        originType = try container.decodeIfPresent(String.self, forKey: .originType) ?? ""
        workRef = try container.decodeIfPresent(String.self, forKey: .workRef) ?? ""
        agentID = try container.decodeIfPresent(String.self, forKey: .agentID) ?? ""
        status = try container.decodeIfPresent(String.self, forKey: .status) ?? ""
        createdAt = try DaemonDateCoding.decodeOptionalDate(container: container, key: .createdAt)
        lastSeenAt = try DaemonDateCoding.decodeOptionalDate(container: container, key: .lastSeenAt)
        updatedAt = try DaemonDateCoding.decodeOptionalDate(container: container, key: .updatedAt)
        attachable = try container.decodeIfPresent(Bool.self, forKey: .attachable) ?? false
    }

    init(
        serverRef: String,
        sessionID: String,
        directory: String,
        project: String,
        originType: String,
        workRef: String,
        agentID: String,
        status: String,
        createdAt: Date?,
        lastSeenAt: Date?,
        updatedAt: Date?,
        attachable: Bool
    ) {
        self.serverRef = serverRef
        self.sessionID = sessionID
        self.directory = directory
        self.project = project
        self.originType = originType
        self.workRef = workRef
        self.agentID = agentID
        self.status = status
        self.createdAt = createdAt
        self.lastSeenAt = lastSeenAt
        self.updatedAt = updatedAt
        self.attachable = attachable
    }
}

struct DaemonToolCallPayload: Codable, Equatable, Sendable {
    let timestamp: Date
    let tool: String
    let title: String
    let input: String
    let status: String
    let durationMs: Int

    private enum CodingKeys: String, CodingKey {
        case timestamp
        case tool
        case title
        case input
        case status
        case durationMs = "duration_ms"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        timestamp = try DaemonDateCoding.decodeDate(container: container, key: .timestamp)
        tool = try container.decodeIfPresent(String.self, forKey: .tool) ?? ""
        title = try container.decodeIfPresent(String.self, forKey: .title) ?? ""
        input = try container.decodeIfPresent(String.self, forKey: .input) ?? ""
        status = try container.decodeIfPresent(String.self, forKey: .status) ?? ""
        durationMs = try container.decodeIfPresent(Int.self, forKey: .durationMs) ?? 0
    }

    init(timestamp: Date, tool: String, title: String, input: String, status: String, durationMs: Int) {
        self.timestamp = timestamp
        self.tool = tool
        self.title = title
        self.input = input
        self.status = status
        self.durationMs = durationMs
    }
}

struct DaemonEventsPayload: Codable, Equatable, Sendable {
    let lines: [String]
    let sessionID: String
    let session: DaemonSessionMetadataPayload
    let lastTS: Int64

    private enum CodingKeys: String, CodingKey {
        case lines
        case sessionID = "session_id"
        case session
        case lastTS = "last_ts"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        lines = try container.decodeIfPresent([String].self, forKey: .lines) ?? []
        sessionID = try container.decodeIfPresent(String.self, forKey: .sessionID) ?? ""
        session = try container.decodeIfPresent(DaemonSessionMetadataPayload.self, forKey: .session) ?? .empty
        lastTS = try container.decodeIfPresent(Int64.self, forKey: .lastTS) ?? 0
    }

    init(lines: [String], sessionID: String, session: DaemonSessionMetadataPayload, lastTS: Int64) {
        self.lines = lines
        self.sessionID = sessionID
        self.session = session
        self.lastTS = lastTS
    }
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

    init(session: URLSession = DefaultDaemonController.makeSession()) {
        self.session = session
    }

    func fetchLifecycle(daemonURL: String) async throws -> DaemonLifecyclePayload {
        guard let url = URL(string: daemonURL + "/api/v1/lifecycle") else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        let request = try Self.authorizedRequest(url: url)
        let (data, response) = try await session.data(for: request)
        return try decodeEnvelope(data, response: response)
    }

    func fetchStatus(daemonURL: String) async throws -> DaemonStatusPayload {
        guard let url = URL(string: daemonURL + "/api/v1/status") else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        let request = try Self.authorizedRequest(url: url)
        let (data, response) = try await session.data(for: request)
        return try decodeEnvelope(data, response: response)
    }

    func fetchAgentDetail(daemonURL: String, agentName: String, limit: Int) async throws -> DaemonAgentDetailPayload {
        guard var components = URLComponents(string: daemonURL + "/api/v1/status/agents/" + agentName.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed)!) else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        if limit > 0 {
            components.queryItems = [URLQueryItem(name: "limit", value: String(limit))]
        }
        guard let url = components.url else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        let request = try Self.authorizedRequest(url: url)
        let (data, response) = try await session.data(for: request)
        return try decodeEnvelope(data, response: response)
    }

    func fetchEvents(daemonURL: String, agentName: String, afterTimestamp: Int64) async throws -> DaemonEventsPayload {
        guard var components = URLComponents(string: daemonURL + "/api/v1/events") else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        var queryItems = [URLQueryItem(name: "agent_name", value: agentName)]
        if afterTimestamp > 0 {
            queryItems.append(URLQueryItem(name: "after_timestamp", value: String(afterTimestamp)))
        }
        components.queryItems = queryItems
        guard let url = components.url else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        let request = try Self.authorizedRequest(url: url)
        let (data, response) = try await session.data(for: request)
        return try decodeEnvelope(data, response: response)
    }

    func requestStop(daemonURL: String, force: Bool) async throws -> DaemonStopResponse {
        let urlString = force ? daemonURL + "/api/v1/shutdown?force=true" : daemonURL + "/api/v1/shutdown"
        guard let url = URL(string: urlString) else {
            throw DaemonControlError.connectionFailed("invalid daemon URL: \(daemonURL)")
        }
        var request = try Self.authorizedRequest(url: url)
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
        let envelope = try DaemonDateCoding.decoder.decode(RPCEnvelope<T>.self, from: data)
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

        let outputQueue = DispatchQueue(label: "aetherflow.daemon-control.output", qos: .userInitiated, attributes: .concurrent)
        let group = DispatchGroup()
        let stdoutResult = PipeReadResult()
        let stderrResult = PipeReadResult()

        group.enter()
        outputQueue.async {
            defer { group.leave() }
            stdoutResult.data = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        }

        group.enter()
        outputQueue.async {
            defer { group.leave() }
            stderrResult.data = stderrPipe.fileHandleForReading.readDataToEndOfFile()
        }

        process.waitUntilExit()
        group.wait()
        return CommandOutput(
            status: process.terminationStatus,
            stdout: String(decoding: stdoutResult.data, as: UTF8.self).trimmed,
            stderr: String(decoding: stderrResult.data, as: UTF8.self).trimmed
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

    private static func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.default
        configuration.timeoutIntervalForRequest = 5
        configuration.timeoutIntervalForResource = 10
        return URLSession(configuration: configuration)
    }

    private static func authorizedRequest(url: URL) throws -> URLRequest {
        var request = URLRequest(url: url)
        if let token = daemonAuthToken(for: url) {
            request.setValue(token, forHTTPHeaderField: "X-Aetherflow-Token")
        }
        return request
    }

    private static func daemonAuthToken(for url: URL) -> String? {
        guard let path = daemonAuthTokenPath(for: url),
              let data = FileManager.default.contents(atPath: path) else {
            return nil
        }
        let token = String(decoding: data, as: UTF8.self).trimmed
        return token.isEmpty ? nil : token
    }

    private static func daemonAuthTokenPath(for url: URL) -> String? {
        let host = normalizedAuthHost(url.host ?? "127.0.0.1")
        guard let port = url.port else {
            return nil
        }
        let configRoot = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config", isDirectory: true)
            .appendingPathComponent("aetherflow", isDirectory: true)
            .appendingPathComponent("auth", isDirectory: true)
        return configRoot.appendingPathComponent("\(host)_\(port).token").path
    }

    private static func normalizedAuthHost(_ host: String) -> String {
        host
            .lowercased()
            .replacingOccurrences(of: ":", with: "_")
            .replacingOccurrences(of: "[", with: "")
            .replacingOccurrences(of: "]", with: "")
    }
}

private final class PipeReadResult: @unchecked Sendable {
    var data = Data()
}

private enum DaemonDateCoding {
    static let decoder: JSONDecoder = {
        JSONDecoder()
    }()

    static func decodeDate<K: CodingKey>(
        container: KeyedDecodingContainer<K>,
        key: K
    ) throws -> Date {
        guard let value = try container.decodeIfPresent(String.self, forKey: key) else {
            return .now
        }
        if let parsed = parseDate(value) {
            return parsed
        }
        throw DecodingError.dataCorruptedError(forKey: key, in: container, debugDescription: "invalid date \(value)")
    }

    static func decodeOptionalDate<K: CodingKey>(
        container: KeyedDecodingContainer<K>,
        key: K
    ) throws -> Date? {
        guard let value = try container.decodeIfPresent(String.self, forKey: key) else {
            return nil
        }
        if let parsed = parseDate(value) {
            return parsed
        }
        throw DecodingError.dataCorruptedError(forKey: key, in: container, debugDescription: "invalid date \(value)")
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

private extension String {
    var trimmed: String {
        trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var nonEmptyTrimmed: String? {
        let value = trimmed
        return value.isEmpty ? nil : value
    }
}
