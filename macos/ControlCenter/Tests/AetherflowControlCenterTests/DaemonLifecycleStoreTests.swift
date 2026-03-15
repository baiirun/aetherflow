import XCTest
@testable import AetherflowControlCenter

actor FakeDaemonController: DaemonControlling {
    var lifecycleResult: Result<DaemonLifecyclePayload, Error>
    var statusResult: Result<DaemonStatusPayload, Error>
    var detailResult: Result<DaemonAgentDetailPayload, Error>
    var eventsResult: Result<DaemonEventsPayload, Error>
    var stopResult: Result<DaemonStopResponse, Error>
    var startResult: Result<DaemonStartReceipt, Error>
    private(set) var stopRequests: [Bool] = []

    init(
        lifecycleResult: Result<DaemonLifecyclePayload, Error>,
        statusResult: Result<DaemonStatusPayload, Error> = .success(
            DaemonStatusPayload(poolSize: 0, poolMode: "", project: "", spawnPolicy: "", agents: [], spawns: [], queue: [], errors: [])
        ),
        detailResult: Result<DaemonAgentDetailPayload, Error> = .success(
            DaemonAgentDetailPayload(
                agent: DaemonAgentStatusPayload(
                    id: "",
                    taskID: "",
                    role: "",
                    pid: 0,
                    spawnTime: .now,
                    taskTitle: "",
                    lastLog: "",
                    sessionID: "",
                    state: "",
                    lifecycleState: "",
                    lastActivityAt: nil,
                    attentionNeeded: false
                ),
                session: .empty,
                toolCalls: [],
                errors: []
            )
        ),
        eventsResult: Result<DaemonEventsPayload, Error> = .success(
            DaemonEventsPayload(lines: [], sessionID: "", session: .empty, lastTS: 0)
        ),
        stopResult: Result<DaemonStopResponse, Error>,
        startResult: Result<DaemonStartReceipt, Error>
    ) {
        self.lifecycleResult = lifecycleResult
        self.statusResult = statusResult
        self.detailResult = detailResult
        self.eventsResult = eventsResult
        self.stopResult = stopResult
        self.startResult = startResult
    }

    func fetchLifecycle(daemonURL: String) async throws -> DaemonLifecyclePayload {
        try lifecycleResult.get()
    }

    func fetchStatus(daemonURL: String) async throws -> DaemonStatusPayload {
        try statusResult.get()
    }

    func fetchAgentDetail(daemonURL: String, agentName: String, limit: Int) async throws -> DaemonAgentDetailPayload {
        try detailResult.get()
    }

    func fetchEvents(daemonURL: String, agentName: String, afterTimestamp: Int64) async throws -> DaemonEventsPayload {
        try eventsResult.get()
    }

    func requestStop(daemonURL: String, force: Bool) async throws -> DaemonStopResponse {
        stopRequests.append(force)
        return try stopResult.get()
    }

    func requestStart(context: ShellBootstrapContext) async throws -> DaemonStartReceipt {
        try startResult.get()
    }

    func stopRequestCount() -> Int {
        stopRequests.count
    }
}

@MainActor
final class DaemonLifecycleStoreTests: XCTestCase {
    func testRequestStopWaitsForDaemonOwnedRefusalBeforePromptingForce() async throws {
        let controller = FakeDaemonController(
            lifecycleResult: .success(Self.lifecycle(activeSessions: 2)),
            stopResult: .success(Self.stopResponse(outcome: "refused", activeSessions: 2, message: "refusing stop with 2 active workload(s) across 2 attached session(s); retry with force after confirmation")),
            startResult: .success(DaemonStartReceipt(message: "daemon started"))
        )
        let bootstrap = ShellBootstrapContext(projectName: "aetherflow", workingDirectory: "/tmp/aetherflow", daemonURL: "http://127.0.0.1:7070", cliPath: "/tmp/aetherflow/af")
        let transportStore = TransportStore(context: bootstrap)
        let store = DaemonLifecycleStore(
            context: bootstrap,
            transportStore: transportStore,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()
        store.requestStop()

        try await eventually {
            store.pendingStopConfirmation?.activeSessions == 2
        }
        let stopRequestCount = await controller.stopRequestCount()
        XCTAssertEqual(stopRequestCount, 1)
    }

    func testSuccessfulStartMovesLifecycleIntoStartingState() async throws {
        let controller = FakeDaemonController(
            lifecycleResult: .failure(DaemonControlError.connectionFailed("dial failed")),
            stopResult: .success(Self.stopResponse(outcome: "stopping", activeSessions: 0, message: "daemon stopping")),
            startResult: .success(DaemonStartReceipt(message: "daemon started (pid 42)"))
        )
        let bootstrap = ShellBootstrapContext(projectName: "aetherflow", workingDirectory: "/tmp/aetherflow", daemonURL: "http://127.0.0.1:7070", cliPath: "/tmp/aetherflow/af")
        let transportStore = TransportStore(context: bootstrap)
        let store = DaemonLifecycleStore(
            context: bootstrap,
            transportStore: transportStore,
            controller: controller,
            isDaemonAbsent: { _ in true },
            autoStartMonitoring: false
        )

        store.requestStart()
        try await eventually {
            store.snapshot.phase == .starting && store.banner?.title == "Start requested"
        }

        XCTAssertEqual(transportStore.snapshot.phase, .unreachable)
        XCTAssertTrue(store.snapshot.statusCopy.contains("Waiting"))
    }

    func testStartTimeoutTransitionsToFailed() async throws {
        let controller = FakeDaemonController(
            lifecycleResult: .failure(DaemonControlError.connectionFailed("dial failed")),
            stopResult: .success(Self.stopResponse(outcome: "stopping", activeSessions: 0, message: "daemon stopping")),
            startResult: .success(DaemonStartReceipt(message: "daemon started (pid 42)"))
        )
        let bootstrap = ShellBootstrapContext(projectName: "aetherflow", workingDirectory: "/tmp/aetherflow", daemonURL: "http://127.0.0.1:7070", cliPath: "/tmp/aetherflow/af")
        let transportStore = TransportStore(context: bootstrap)
        let store = DaemonLifecycleStore(
            context: bootstrap,
            transportStore: transportStore,
            controller: controller,
            isDaemonAbsent: { _ in true },
            pollIntervalNanoseconds: 1_000_000,
            startupTimeout: 0.01,
            autoStartMonitoring: false
        )

        store.requestStart()
        try await Task.sleep(nanoseconds: 30_000_000)
        await store.refresh()

        XCTAssertEqual(store.snapshot.phase, .failed)
        XCTAssertEqual(store.banner?.title, "Start timed out")
        XCTAssertEqual(transportStore.snapshot.phase, .unreachable)
    }

    func testRefusedStopSurfacesDaemonOwnedConfirmation() async throws {
        let controller = FakeDaemonController(
            lifecycleResult: .success(Self.lifecycle(activeSessions: 0)),
            stopResult: .success(Self.stopResponse(outcome: "refused", activeSessions: 1, message: "refusing stop with 1 active workload(s) across 1 attached session(s); retry with force after confirmation")),
            startResult: .success(DaemonStartReceipt(message: "daemon started"))
        )
        let bootstrap = ShellBootstrapContext(projectName: "aetherflow", workingDirectory: "/tmp/aetherflow", daemonURL: "http://127.0.0.1:7070", cliPath: "/tmp/aetherflow/af")
        let transportStore = TransportStore(context: bootstrap)
        let store = DaemonLifecycleStore(
            context: bootstrap,
            transportStore: transportStore,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()
        store.requestStop()
        try await eventually {
            store.pendingStopConfirmation?.activeSessions == 1 && store.banner?.title == "Stop refused"
        }

        let stopRequestCount = await controller.stopRequestCount()
        XCTAssertEqual(stopRequestCount, 1)
    }

    func testDetectUsesCLIPathOverride() {
        let context = ShellBootstrapContext.detect(
            environment: [
                "AETHERFLOW_PROJECT": "control-room",
                "AETHERFLOW_WORKING_DIRECTORY": "/tmp/control-room",
                "AETHERFLOW_DAEMON_URL": "http://127.0.0.1:7099",
                "AETHERFLOW_CLI_PATH": "/tmp/bin/af",
            ],
            currentDirectoryPath: "/ignored"
        )

        XCTAssertEqual(context.cliPath, "/tmp/bin/af")
        XCTAssertEqual(context.daemonURL, "http://127.0.0.1:7099")
    }

    func testRefreshDiagnosticsCallOutNonManualDaemonTargetMismatch() async throws {
        let controller = FakeDaemonController(
            lifecycleResult: .success(
                DaemonLifecyclePayload(
                    state: "running",
                    daemonURL: "http://127.0.0.1:7103",
                    project: "control-room",
                    serverURL: "http://127.0.0.1:4096",
                    spawnPolicy: "auto",
                    activeSessionCount: 0,
                    activeSessionIDs: [],
                    lastError: "",
                    updatedAt: .now
                )
            ),
            stopResult: .success(Self.stopResponse(outcome: "stopping", activeSessions: 0, message: "daemon stopping")),
            startResult: .success(DaemonStartReceipt(message: "daemon started"))
        )
        let bootstrap = ShellBootstrapContext(
            projectName: "control-room",
            workingDirectory: "/tmp/control-room",
            daemonURL: "http://127.0.0.1:7070",
            cliPath: "/tmp/control-room/af"
        )
        let transportStore = TransportStore(context: bootstrap)
        let store = DaemonLifecycleStore(
            context: bootstrap,
            transportStore: transportStore,
            controller: controller,
            isDaemonAbsent: { _ in false },
            autoStartMonitoring: false
        )

        await store.refresh()

        XCTAssertTrue(transportStore.snapshot.note.contains("Spawn policy: auto."))
        XCTAssertTrue(transportStore.snapshot.note.contains("non-manual daemon"))
        XCTAssertTrue(transportStore.snapshot.note.contains("7103"))
    }

    private func eventually(
        timeoutNanoseconds: UInt64 = 500_000_000,
        pollNanoseconds: UInt64 = 20_000_000,
        condition: @escaping @MainActor () -> Bool
    ) async throws {
        let deadline = DispatchTime.now().uptimeNanoseconds + timeoutNanoseconds
        while DispatchTime.now().uptimeNanoseconds < deadline {
            if condition() {
                return
            }
            try await Task.sleep(nanoseconds: pollNanoseconds)
        }
        XCTFail("condition was not satisfied before timeout")
    }

    private static func lifecycle(
        state: String = "running",
        activeSessions: Int
    ) -> DaemonLifecyclePayload {
        DaemonLifecyclePayload(
            state: state,
            daemonURL: "http://127.0.0.1:7070",
            project: "aetherflow",
            serverURL: "http://127.0.0.1:4096",
            spawnPolicy: "manual",
            activeSessionCount: activeSessions,
            activeSessionIDs: Array(repeating: "sess-1", count: activeSessions),
            lastError: "",
            updatedAt: Date.now
        )
    }

    private static func stopResponse(
        outcome: String,
        activeSessions: Int,
        message: String
    ) -> DaemonStopResponse {
        DaemonStopResponse(
            outcome: outcome,
            status: lifecycle(activeSessions: activeSessions),
            message: message
        )
    }
}
