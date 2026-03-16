import XCTest
@testable import AetherflowControlCenter

final class SessionHandoffLauncherTests: XCTestCase {
    func testTerminalCommandUsesRecordedSessionMetadata() throws {
        let command = try SessionHandoffLauncher.terminalCommand(
            session: Self.sessionMetadata(),
            transport: Self.transportSnapshot(
                workingDirectory: "/tmp",
                cliPath: "/tmp/tools/af runner"
            )
        )

        XCTAssertEqual(
            command,
            "cd '/tmp' && '/tmp/tools/af runner' session attach 'ses-123' --server 'http://127.0.0.1:4096'"
        )
    }

    func testTerminalCommandRejectsUnavailableSessions() {
        XCTAssertThrowsError(
            try SessionHandoffLauncher.terminalCommand(
                session: Self.sessionMetadata(attachable: false),
                transport: Self.transportSnapshot()
            )
        ) { error in
            XCTAssertEqual(
                error as? SessionHandoffLaunchError,
                .unavailable("Opencode handoff is unavailable for this session. The daemon has not exposed an attachable route yet.")
            )
        }
    }

    func testTerminalCommandRejectsMissingSessionID() {
        XCTAssertThrowsError(
            try SessionHandoffLauncher.terminalCommand(
                session: Self.sessionMetadata(sessionID: ""),
                transport: Self.transportSnapshot()
            )
        ) { error in
            XCTAssertEqual(
                error as? SessionHandoffLaunchError,
                .invalidMetadata("Cannot launch Opencode handoff because session_id is missing from the daemon metadata.")
            )
        }
    }

    func testLaunchInvokesOsascriptForTerminalHandoff() async throws {
        let recorder = RunnerRecorder()
        let launcher = SessionHandoffLauncher { executable, arguments, currentDirectory in
            recorder.calls.append(
                RunnerCall(
                    executable: executable,
                    arguments: arguments,
                    currentDirectory: currentDirectory
                )
            )
            return SessionHandoffCommandOutput(status: 0, stdout: "", stderr: "")
        }

        try await launcher.launch(
            session: Self.sessionMetadata(),
            transport: Self.transportSnapshot()
        )

        XCTAssertEqual(recorder.calls.count, 1)
        XCTAssertEqual(recorder.calls[0].executable, "/usr/bin/osascript")
        XCTAssertEqual(recorder.calls[0].currentDirectory, "/tmp")
        XCTAssertEqual(
            Array(recorder.calls[0].arguments.prefix(4)),
            ["-e", "tell application \"Terminal\"", "-e", "activate"]
        )
        XCTAssertTrue(
            recorder.calls[0].arguments.contains(
                "do script \"cd '/tmp' && '/tmp/tools/af' session attach 'ses-123' --server 'http://127.0.0.1:4096'\""
            )
        )
    }

    func testLaunchSurfacesAttachCommandFailure() async {
        let launcher = SessionHandoffLauncher { _, _, _ in
            SessionHandoffCommandOutput(status: 1, stdout: "", stderr: "attach failed")
        }

        await XCTAssertThrowsErrorAsync(
            try await launcher.launch(
                session: Self.sessionMetadata(),
                transport: Self.transportSnapshot()
            )
        ) { error in
            XCTAssertEqual(
                error as? SessionHandoffLaunchError,
                .launchFailed("attach failed")
            )
        }
    }

    static func transportSnapshot(
        workingDirectory: String = "/tmp",
        cliPath: String = "/tmp/tools/af"
    ) -> TransportSnapshot {
        TransportSnapshot(
            phase: .connected,
            projectName: "aetherflow",
            workingDirectory: workingDirectory,
            daemonURL: "http://127.0.0.1:7070",
            cliPath: cliPath,
            daemonTargetReason: "test",
            note: "test"
        )
    }

    static func sessionMetadata(
        sessionID: String = "ses-123",
        serverRef: String = "http://127.0.0.1:4096",
        attachable: Bool = true
    ) -> DaemonSessionMetadataPayload {
        DaemonSessionMetadataPayload(
            serverRef: serverRef,
            sessionID: sessionID,
            directory: "/tmp/aetherflow",
            project: "aetherflow",
            originType: "agent",
            workRef: "ts-030640",
            agentID: "agent-1",
            status: "running",
            createdAt: nil,
            lastSeenAt: nil,
            updatedAt: nil,
            attachable: attachable
        )
    }
}

@MainActor
final class SessionHandoffStoreTests: XCTestCase {
    func testRequestLaunchTransitionsToLaunchingImmediately() async {
        let launcher = TestHandoffLauncher { _, _ in
            try await Task.sleep(nanoseconds: 200_000_000)
        }
        let store = SessionHandoffStore(launcher: launcher)
        let detail = Self.detail()
        let transport = Self.transportSnapshot()

        store.requestLaunch(detail: detail, transport: transport)

        XCTAssertEqual(store.phase(for: detail), .launching)
    }

    func testRequestLaunchPublishesFailureForSelection() async {
        let launcher = TestHandoffLauncher { _, _ in
            throw SessionHandoffLaunchError.launchFailed("attach failed")
        }
        let store = SessionHandoffStore(launcher: launcher)
        let detail = Self.detail()
        let transport = Self.transportSnapshot()

        store.requestLaunch(detail: detail, transport: transport)
        await waitForPhase(store, detail: detail) { phase in
            phase == .failure("attach failed")
        }

        XCTAssertEqual(store.phase(for: detail), .failure("attach failed"))
    }

    private static func detail(sessionID: String = "ses-123") -> MonitoringSelectionDetail {
        MonitoringSelectionDetail(
            workloadID: "agent-1",
            session: SessionHandoffLauncherTests.sessionMetadata(sessionID: sessionID),
            agent: DaemonAgentStatusPayload(
                id: "agent-1",
                taskID: "ts-030640",
                role: "worker",
                pid: 42,
                spawnTime: .now,
                taskTitle: "Launch handoff",
                lastLog: "working",
                sessionID: sessionID,
                state: "running",
                lifecycleState: "running",
                lastActivityAt: .now,
                attentionNeeded: false
            ),
            toolCalls: [],
            eventLines: [],
            lastEventTimestamp: 0,
            errors: [],
            isLive: true
        )
    }

    private static func transportSnapshot() -> TransportSnapshot {
        SessionHandoffLauncherTests.transportSnapshot()
    }

    private func waitForPhase(
        _ store: SessionHandoffStore,
        detail: MonitoringSelectionDetail,
        matches: @escaping (SessionHandoffPhase) -> Bool
    ) async {
        for _ in 0..<20 {
            if matches(store.phase(for: detail)) {
                return
            }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTFail("Timed out waiting for expected handoff phase.")
    }
}

private final class RunnerRecorder: @unchecked Sendable {
    var calls: [RunnerCall] = []
}

private struct TestHandoffLauncher: SessionHandoffLaunching {
    let launchImpl: @Sendable (DaemonSessionMetadataPayload, TransportSnapshot) async throws -> Void

    init(_ launchImpl: @escaping @Sendable (DaemonSessionMetadataPayload, TransportSnapshot) async throws -> Void) {
        self.launchImpl = launchImpl
    }

    func launch(session: DaemonSessionMetadataPayload, transport: TransportSnapshot) async throws {
        try await launchImpl(session, transport)
    }
}

private struct RunnerCall: Equatable {
    let executable: String
    let arguments: [String]
    let currentDirectory: String
}

private func XCTAssertThrowsErrorAsync<T>(
    _ expression: @autoclosure () async throws -> T,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line,
    _ errorHandler: (Error) -> Void = { _ in }
) async {
    do {
        _ = try await expression()
        XCTFail(message(), file: file, line: line)
    } catch {
        errorHandler(error)
    }
}
