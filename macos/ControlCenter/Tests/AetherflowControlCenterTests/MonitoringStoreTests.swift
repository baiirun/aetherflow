import XCTest
@testable import AetherflowControlCenter

actor RecordingDaemonController: DaemonControlling {
    var lifecycleResult: Result<DaemonLifecyclePayload, Error> = .failure(DaemonControlError.connectionFailed("unused"))
    var stopResult: Result<DaemonStopResponse, Error> = .failure(DaemonControlError.connectionFailed("unused"))
    var startResult: Result<DaemonStartReceipt, Error> = .failure(DaemonControlError.connectionFailed("unused"))

    private var statusResults: [Result<DaemonStatusPayload, Error>]
    private var detailResults: [Result<DaemonAgentDetailPayload, Error>]
    private var eventResults: [Result<DaemonEventsPayload, Error>]
    private(set) var eventAfterTimestamps: [Int64] = []

    init(
        statusResults: [Result<DaemonStatusPayload, Error>],
        detailResults: [Result<DaemonAgentDetailPayload, Error>],
        eventResults: [Result<DaemonEventsPayload, Error>]
    ) {
        self.statusResults = statusResults
        self.detailResults = detailResults
        self.eventResults = eventResults
    }

    func fetchLifecycle(daemonURL: String) async throws -> DaemonLifecyclePayload {
        try lifecycleResult.get()
    }

    func fetchStatus(daemonURL: String) async throws -> DaemonStatusPayload {
        try nextResult(from: &statusResults).get()
    }

    func fetchAgentDetail(daemonURL: String, agentName: String, limit: Int) async throws -> DaemonAgentDetailPayload {
        try nextResult(from: &detailResults).get()
    }

    func fetchEvents(daemonURL: String, agentName: String, afterTimestamp: Int64) async throws -> DaemonEventsPayload {
        eventAfterTimestamps.append(afterTimestamp)
        return try nextResult(from: &eventResults).get()
    }

    func requestStop(daemonURL: String, force: Bool) async throws -> DaemonStopResponse {
        try stopResult.get()
    }

    func requestStart(context: ShellBootstrapContext) async throws -> DaemonStartReceipt {
        try startResult.get()
    }

    func recordedEventAfterTimestamps() -> [Int64] {
        eventAfterTimestamps
    }

    private func nextResult<T>(from results: inout [Result<T, Error>]) -> Result<T, Error> {
        precondition(!results.isEmpty, "expected queued controller result")
        return results.removeFirst()
    }
}

@MainActor
final class MonitoringStoreTests: XCTestCase {
    func testRefreshLoadsStatusAndDetailFromHTTPContract() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [.success(Self.status(taskTitle: "Implement HTTP transport"))],
            detailResults: [.success(Self.detail())],
            eventResults: [.success(Self.events(lines: ["session.created"], lastTS: 101))]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()

        XCTAssertEqual(store.snapshot.phase, .connected)
        XCTAssertEqual(store.snapshot.workloads.map(\.id), ["agent-1"])
        XCTAssertEqual(store.snapshot.selectedWorkloadID, "agent-1")
        XCTAssertEqual(store.snapshot.selectedDetail?.eventLines, ["session.created"])
        XCTAssertEqual(store.snapshot.selectedDetail?.lastEventTimestamp, 101)
    }

    func testReconnectForcesAuthoritativeEventReloadBeforeCursorResumes() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(Self.status(taskTitle: "Initial task")),
                .failure(URLError(.cannotConnectToHost)),
                .success(Self.status(taskTitle: "Recovered task")),
            ],
            detailResults: [
                .success(Self.detail()),
                .success(Self.detail()),
            ],
            eventResults: [
                .success(Self.events(lines: ["session.created"], lastTS: 101)),
                .success(Self.events(lines: ["session.created", "task.updated"], lastTS: 220)),
            ]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { error in
                guard let urlError = error as? URLError else {
                    return false
                }
                return urlError.code == .cannotConnectToHost
            },
            autoStartMonitoring: false
        )

        await store.refresh()
        await store.refresh()
        XCTAssertEqual(store.snapshot.phase, .reconnecting)

        await store.refresh()

        XCTAssertEqual(store.snapshot.phase, .connected)
        XCTAssertEqual(store.snapshot.selectedDetail?.eventLines, ["session.created", "task.updated"])
        let eventAfterTimestamps = await controller.recordedEventAfterTimestamps()
        XCTAssertEqual(eventAfterTimestamps, [0, 0])
    }

    private static var bootstrap: ShellBootstrapContext {
        ShellBootstrapContext(
            projectName: "aetherflow",
            workingDirectory: "/tmp/aetherflow",
            daemonURL: "http://127.0.0.1:7070",
            cliPath: "/tmp/aetherflow/af"
        )
    }

    private static func status(taskTitle: String) -> DaemonStatusPayload {
        DaemonStatusPayload(
            poolSize: 1,
            poolMode: "active",
            project: "aetherflow",
            spawnPolicy: "manual",
            agents: [
                DaemonAgentStatusPayload(
                    id: "agent-1",
                    taskID: "ts-c9cdd2",
                    role: "worker",
                    pid: 42,
                    spawnTime: .now,
                    taskTitle: taskTitle,
                    lastLog: "working",
                    sessionID: "ses-1",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: .now,
                    attentionNeeded: false
                )
            ],
            spawns: [],
            queue: [
                DaemonQueueTaskPayload(id: "ts-next", priority: 1, title: "Next task")
            ],
            errors: []
        )
    }

    private static func detail() -> DaemonAgentDetailPayload {
        DaemonAgentDetailPayload(
            agent: DaemonAgentStatusPayload(
                id: "agent-1",
                taskID: "ts-c9cdd2",
                role: "worker",
                pid: 42,
                spawnTime: .now,
                taskTitle: "Implement HTTP transport",
                lastLog: "working",
                sessionID: "ses-1",
                state: "running",
                lifecycleState: "running",
                lastActivityAt: .now,
                attentionNeeded: false
            ),
            session: DaemonSessionMetadataPayload(
                serverRef: "http://127.0.0.1:4096",
                sessionID: "ses-1",
                directory: "/tmp/aetherflow",
                project: "aetherflow",
                originType: "pool",
                workRef: "ts-c9cdd2",
                agentID: "agent-1",
                status: "active",
                createdAt: .now,
                lastSeenAt: .now,
                updatedAt: .now,
                attachable: true
            ),
            toolCalls: [],
            errors: []
        )
    }

    private static func events(lines: [String], lastTS: Int64) -> DaemonEventsPayload {
        DaemonEventsPayload(
            lines: lines,
            sessionID: "ses-1",
            session: DaemonSessionMetadataPayload(
                serverRef: "http://127.0.0.1:4096",
                sessionID: "ses-1",
                directory: "/tmp/aetherflow",
                project: "aetherflow",
                originType: "pool",
                workRef: "ts-c9cdd2",
                agentID: "agent-1",
                status: "active",
                createdAt: .now,
                lastSeenAt: .now,
                updatedAt: .now,
                attachable: true
            ),
            lastTS: lastTS
        )
    }
}
