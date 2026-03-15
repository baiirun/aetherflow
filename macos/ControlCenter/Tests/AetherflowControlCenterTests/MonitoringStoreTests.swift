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
    private(set) var detailAgentNames: [String] = []
    private(set) var eventAgentNames: [String] = []

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
        detailAgentNames.append(agentName)
        return try nextResult(from: &detailResults).get()
    }

    func fetchEvents(daemonURL: String, agentName: String, afterTimestamp: Int64) async throws -> DaemonEventsPayload {
        eventAfterTimestamps.append(afterTimestamp)
        eventAgentNames.append(agentName)
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

    func recordedDetailAgentNames() -> [String] {
        detailAgentNames
    }

    func recordedEventAgentNames() -> [String] {
        eventAgentNames
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
            eventResults: [.success(Self.events(lines: ["\u{001B}[2m12:00:00\u{001B}[0m  session.created"], lastTS: 101))]
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
        XCTAssertEqual(store.snapshot.selectedDetail?.eventLines, ["12:00:00  session.created"])
        XCTAssertEqual(store.snapshot.selectedDetail?.lastEventTimestamp, 101)
        XCTAssertEqual(store.snapshot.selectedDetail?.isLive, true)
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

    func testSelectingDifferentWorkloadReloadsDetailForThatSelection() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(Self.statusWithAgentAndSpawn()),
                .success(Self.statusWithAgentAndSpawn()),
            ],
            detailResults: [
                .success(Self.detail(agentID: "agent-1", workRef: "ts-c9cdd2", sessionID: "ses-1")),
                .success(Self.detail(agentID: "spawn-1", workRef: "manual-spawn", sessionID: "ses-2")),
            ],
            eventResults: [
                .success(Self.events(lines: ["session.created"], sessionID: "ses-1", workRef: "ts-c9cdd2", agentID: "agent-1", lastTS: 101)),
                .success(Self.events(lines: ["spawn.created"], sessionID: "ses-2", workRef: "manual-spawn", agentID: "spawn-1", lastTS: 202)),
            ]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()
        store.selectWorkload(id: "spawn-1")
        await store.refresh()

        XCTAssertEqual(store.snapshot.selectedWorkloadID, "spawn-1")
        XCTAssertEqual(store.snapshot.selectedDetail?.workloadID, "spawn-1")
        XCTAssertEqual(store.snapshot.selectedDetail?.session.sessionID, "ses-2")

        let detailAgentNames = await controller.recordedDetailAgentNames()
        let eventAgentNames = await controller.recordedEventAgentNames()
        XCTAssertEqual(detailAgentNames, ["agent-1", "spawn-1"])
        XCTAssertEqual(eventAgentNames, ["agent-1", "spawn-1"])
    }

    func testSelectionTracksSameSessionWhenWorkloadIDChanges() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(Self.status(taskTitle: "Initial task")),
                .success(Self.status(agentID: "agent-2", taskTitle: "Recovered task", sessionID: "ses-1")),
            ],
            detailResults: [
                .success(Self.detail(agentID: "agent-1", workRef: "ts-c9cdd2", sessionID: "ses-1")),
                .success(Self.detail(agentID: "agent-2", workRef: "ts-c9cdd2", sessionID: "ses-1")),
            ],
            eventResults: [
                .success(Self.events(lines: ["session.created"], sessionID: "ses-1", workRef: "ts-c9cdd2", agentID: "agent-1", lastTS: 101)),
                .success(Self.events(lines: ["session.reclaimed"], sessionID: "ses-1", workRef: "ts-c9cdd2", agentID: "agent-2", lastTS: 202)),
            ]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()
        await store.refresh()

        XCTAssertEqual(store.snapshot.selectedWorkloadID, "agent-2")
        XCTAssertEqual(store.snapshot.selectedDetail?.workloadID, "agent-2")
        XCTAssertEqual(store.snapshot.selectedDetail?.session.sessionID, "ses-1")

        let detailAgentNames = await controller.recordedDetailAgentNames()
        XCTAssertEqual(detailAgentNames, ["agent-1", "agent-2"])
    }

    func testSelectedDetailIsRetainedWhenWorkloadLeavesLiveStatus() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(Self.status(taskTitle: "Live task")),
                .success(Self.statusWithoutWorkloads()),
                .success(Self.statusWithoutWorkloads()),
            ],
            detailResults: [
                .success(Self.detail()),
            ],
            eventResults: [
                .success(Self.events(lines: ["session.created", "agent finished"], lastTS: 101)),
            ]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()
        await store.refresh()
        await store.refresh()

        XCTAssertEqual(store.snapshot.phase, .connected)
        XCTAssertEqual(store.snapshot.workloads.count, 0)
        XCTAssertEqual(store.snapshot.selectedWorkloadID, "agent-1")
        XCTAssertEqual(store.snapshot.selectedDetail?.workloadID, "agent-1")
        XCTAssertEqual(store.snapshot.selectedDetail?.session.sessionID, "ses-1")
        XCTAssertEqual(store.snapshot.selectedDetail?.eventLines, ["session.created", "agent finished"])
        XCTAssertEqual(store.snapshot.selectedDetail?.isLive, false)
        XCTAssertEqual(store.snapshot.selectedDetail?.agent.lifecycleState, "exited")

        let detailAgentNames = await controller.recordedDetailAgentNames()
        let eventAgentNames = await controller.recordedEventAgentNames()
        XCTAssertEqual(detailAgentNames, ["agent-1"])
        XCTAssertEqual(eventAgentNames, ["agent-1"])
    }

    func testRefreshPreservesExistingOrderAndAppendsNewWorkloads() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(Self.statusWithAgentAndSpawn()),
                .success(Self.statusWithAdditionalAgent()),
            ],
            detailResults: [
                .success(Self.detail(agentID: "agent-1", workRef: "ts-c9cdd2", sessionID: "ses-1")),
                .success(Self.detail(agentID: "agent-1", workRef: "ts-c9cdd2", sessionID: "ses-1")),
            ],
            eventResults: [
                .success(Self.events(lines: ["session.created"], sessionID: "ses-1", workRef: "ts-c9cdd2", agentID: "agent-1", lastTS: 101)),
                .success(Self.events(lines: ["task.updated"], sessionID: "ses-1", workRef: "ts-c9cdd2", agentID: "agent-1", lastTS: 120)),
            ]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()
        await store.refresh()

        XCTAssertEqual(store.snapshot.workloads.map(\.id), ["agent-1", "spawn-1", "agent-2"])
    }

    func testRefreshOrdersNewWorkloadsByAttentionKindSpawnTimeAndID() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(Self.statusForNewWorkloadOrdering()),
            ],
            detailResults: [
                .success(Self.detail(agentID: "agent-attn", workRef: "ts-attn", sessionID: "ses-attn")),
            ],
            eventResults: [
                .success(Self.events(lines: ["session.created"], sessionID: "ses-attn", workRef: "ts-attn", agentID: "agent-attn", lastTS: 101)),
            ]
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()

        XCTAssertEqual(
            store.snapshot.workloads.map(\.id),
            ["agent-attn", "agent-newer", "agent-older", "spawn-a", "spawn-b"]
        )
    }

    func testRefreshNoteCallsOutNonManualDaemonTarget() async throws {
        let bootstrap = Self.bootstrap
        let controller = RecordingDaemonController(
            statusResults: [
                .success(
                    DaemonStatusPayload(
                        poolSize: 1,
                        poolMode: "active",
                        project: "control-room",
                        spawnPolicy: "auto",
                        agents: [],
                        spawns: [],
                        queue: [],
                        errors: []
                    )
                )
            ],
            detailResults: [],
            eventResults: []
        )
        let store = MonitoringStore(
            context: bootstrap,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()

        XCTAssertTrue(store.snapshot.note.contains("Spawn policy: auto."))
        XCTAssertTrue(store.snapshot.note.contains("not serving a manual daemon"))
    }

    private static var bootstrap: ShellBootstrapContext {
        ShellBootstrapContext(
            projectName: "aetherflow",
            workingDirectory: "/tmp/aetherflow",
            daemonURL: "http://127.0.0.1:7070",
            cliPath: "/tmp/aetherflow/af"
        )
    }

    private static func status(
        agentID: String = "agent-1",
        taskTitle: String,
        sessionID: String = "ses-1"
    ) -> DaemonStatusPayload {
        DaemonStatusPayload(
            poolSize: 1,
            poolMode: "active",
            project: "aetherflow",
            spawnPolicy: "manual",
            agents: [
                DaemonAgentStatusPayload(
                    id: agentID,
                    taskID: "ts-c9cdd2",
                    role: "worker",
                    pid: 42,
                    spawnTime: .now,
                    taskTitle: taskTitle,
                    lastLog: "working",
                    sessionID: sessionID,
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

    private static func statusWithAdditionalAgent() -> DaemonStatusPayload {
        DaemonStatusPayload(
            poolSize: 2,
            poolMode: "active",
            project: "aetherflow",
            spawnPolicy: "manual",
            agents: [
                DaemonAgentStatusPayload(
                    id: "agent-1",
                    taskID: "ts-c9cdd2",
                    role: "worker",
                    pid: 42,
                    spawnTime: .now.addingTimeInterval(-120),
                    taskTitle: "Implement HTTP transport",
                    lastLog: "working",
                    sessionID: "ses-1",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: .now,
                    attentionNeeded: false
                ),
                DaemonAgentStatusPayload(
                    id: "agent-2",
                    taskID: "ts-next",
                    role: "worker",
                    pid: 43,
                    spawnTime: .now,
                    taskTitle: "Follow-up task",
                    lastLog: "queued for handoff",
                    sessionID: "ses-3",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: .now,
                    attentionNeeded: true
                ),
            ],
            spawns: [
                DaemonSpawnStatusPayload(
                    spawnID: "spawn-1",
                    pid: 99,
                    sessionID: "ses-2",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: .now,
                    attentionNeeded: false,
                    prompt: "Manual validation run",
                    spawnTime: .now.addingTimeInterval(-60),
                    exitedAt: nil
                )
            ],
            queue: [],
            errors: []
        )
    }

    private static func statusWithAgentAndSpawn() -> DaemonStatusPayload {
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
                    taskTitle: "Implement HTTP transport",
                    lastLog: "working",
                    sessionID: "ses-1",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: .now,
                    attentionNeeded: false
                )
            ],
            spawns: [
                DaemonSpawnStatusPayload(
                    spawnID: "spawn-1",
                    pid: 99,
                    sessionID: "ses-2",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: .now,
                    attentionNeeded: false,
                    prompt: "Manual validation run",
                    spawnTime: .now,
                    exitedAt: nil
                )
            ],
            queue: [],
            errors: []
        )
    }

    private static func statusForNewWorkloadOrdering() -> DaemonStatusPayload {
        let baseTime = Date(timeIntervalSince1970: 1_700_000_000)

        return DaemonStatusPayload(
            poolSize: 3,
            poolMode: "active",
            project: "aetherflow",
            spawnPolicy: "manual",
            agents: [
                DaemonAgentStatusPayload(
                    id: "agent-attn",
                    taskID: "ts-attn",
                    role: "worker",
                    pid: 0,
                    spawnTime: baseTime.addingTimeInterval(-300),
                    taskTitle: "Needs attention",
                    lastLog: "waiting for reconnect",
                    sessionID: "ses-attn",
                    state: "pending",
                    lifecycleState: "starting",
                    lastActivityAt: nil,
                    attentionNeeded: true
                ),
                DaemonAgentStatusPayload(
                    id: "agent-newer",
                    taskID: "ts-newer",
                    role: "worker",
                    pid: 41,
                    spawnTime: baseTime.addingTimeInterval(-60),
                    taskTitle: "Newest agent",
                    lastLog: "running",
                    sessionID: "ses-newer",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: baseTime,
                    attentionNeeded: false
                ),
                DaemonAgentStatusPayload(
                    id: "agent-older",
                    taskID: "ts-older",
                    role: "worker",
                    pid: 40,
                    spawnTime: baseTime.addingTimeInterval(-120),
                    taskTitle: "Older agent",
                    lastLog: "running",
                    sessionID: "ses-older",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: baseTime.addingTimeInterval(-10),
                    attentionNeeded: false
                ),
            ],
            spawns: [
                DaemonSpawnStatusPayload(
                    spawnID: "spawn-b",
                    pid: 51,
                    sessionID: "ses-spawn-b",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: baseTime,
                    attentionNeeded: false,
                    prompt: "Spawn B",
                    spawnTime: baseTime.addingTimeInterval(-180),
                    exitedAt: nil
                ),
                DaemonSpawnStatusPayload(
                    spawnID: "spawn-a",
                    pid: 50,
                    sessionID: "ses-spawn-a",
                    state: "running",
                    lifecycleState: "running",
                    lastActivityAt: baseTime,
                    attentionNeeded: false,
                    prompt: "Spawn A",
                    spawnTime: baseTime.addingTimeInterval(-180),
                    exitedAt: nil
                )
            ],
            queue: [],
            errors: []
        )
    }

    private static func statusWithoutWorkloads() -> DaemonStatusPayload {
        DaemonStatusPayload(
            poolSize: 0,
            poolMode: "active",
            project: "aetherflow",
            spawnPolicy: "manual",
            agents: [],
            spawns: [],
            queue: [],
            errors: []
        )
    }

    private static func detail(
        agentID: String = "agent-1",
        workRef: String = "ts-c9cdd2",
        sessionID: String = "ses-1"
    ) -> DaemonAgentDetailPayload {
        DaemonAgentDetailPayload(
            agent: DaemonAgentStatusPayload(
                id: agentID,
                taskID: workRef,
                role: agentID == "spawn-1" ? "spawn" : "worker",
                pid: 42,
                spawnTime: .now,
                taskTitle: workRef == "manual-spawn" ? "Manual validation run" : "Implement HTTP transport",
                lastLog: "working",
                sessionID: sessionID,
                state: "running",
                lifecycleState: "running",
                lastActivityAt: .now,
                attentionNeeded: false
            ),
            session: DaemonSessionMetadataPayload(
                serverRef: "http://127.0.0.1:4096",
                sessionID: sessionID,
                directory: "/tmp/aetherflow",
                project: "aetherflow",
                originType: agentID == "spawn-1" ? "spawn" : "pool",
                workRef: workRef,
                agentID: agentID,
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

    private static func events(
        lines: [String],
        sessionID: String = "ses-1",
        workRef: String = "ts-c9cdd2",
        agentID: String = "agent-1",
        lastTS: Int64
    ) -> DaemonEventsPayload {
        DaemonEventsPayload(
            lines: lines,
            sessionID: sessionID,
            session: DaemonSessionMetadataPayload(
                serverRef: "http://127.0.0.1:4096",
                sessionID: sessionID,
                directory: "/tmp/aetherflow",
                project: "aetherflow",
                originType: agentID == "spawn-1" ? "spawn" : "pool",
                workRef: workRef,
                agentID: agentID,
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
