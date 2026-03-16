import XCTest
@testable import AetherflowControlCenter

final class SessionHandoffLauncherTests: XCTestCase {
    func testTerminalCommandUsesRecordedSessionMetadata() throws {
        let command = try SessionHandoffLauncher.terminalCommand(
            session: Self.sessionMetadata(),
            transport: Self.transportSnapshot(
                workingDirectory: "/tmp/Aetherflow Workspace",
                cliPath: "/tmp/tools/af"
            )
        )

        XCTAssertEqual(
            command,
            "cd '/tmp/Aetherflow Workspace' && '/tmp/tools/af' session attach 'ses-123' --server 'http://127.0.0.1:4096'"
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
        XCTAssertEqual(recorder.calls[0].currentDirectory, "/tmp/aetherflow")
        XCTAssertEqual(
            Array(recorder.calls[0].arguments.prefix(4)),
            ["-e", "tell application \"Terminal\"", "-e", "activate"]
        )
        XCTAssertTrue(
            recorder.calls[0].arguments.contains(
                "do script \"cd '/tmp/aetherflow' && '/tmp/aetherflow/af' session attach 'ses-123' --server 'http://127.0.0.1:4096'\""
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

    private static func transportSnapshot(
        workingDirectory: String = "/tmp/aetherflow",
        cliPath: String = "/tmp/aetherflow/af"
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

    private static func sessionMetadata(
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

private final class RunnerRecorder: @unchecked Sendable {
    var calls: [RunnerCall] = []
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
