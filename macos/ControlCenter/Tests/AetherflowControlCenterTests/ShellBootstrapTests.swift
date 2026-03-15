import XCTest
@testable import AetherflowControlCenter

final class ShellBootstrapTests: XCTestCase {
    func testDefaultManualDaemonURLUsesGlobalPort() {
        XCTAssertEqual(ShellBootstrapContext.defaultManualDaemonURL(), "http://127.0.0.1:7070")
    }

    func testDetectUsesEnvironmentOverrides() {
        let context = ShellBootstrapContext.detect(
            environment: [
                "AETHERFLOW_PROJECT": "control-room",
                "AETHERFLOW_WORKING_DIRECTORY": "/tmp/control-room",
                "AETHERFLOW_DAEMON_URL": "http://127.0.0.1:7099",
            ],
            currentDirectoryPath: "/ignored"
        )

        XCTAssertEqual(context.projectName, "control-room")
        XCTAssertEqual(context.workingDirectory, "/tmp/control-room")
        XCTAssertEqual(context.daemonURL, "http://127.0.0.1:7099")
        XCTAssertTrue(context.daemonTargetReason.contains("AETHERFLOW_DAEMON_URL"))
        XCTAssertEqual(context.daemonListenAddressOverride, "127.0.0.1:7099")
    }

    func testDefaultProjectNameUsesCurrentDirectory() {
        XCTAssertEqual(
            ShellBootstrapContext.defaultProjectName(for: "/Users/byronguina/code/aetherflow"),
            "aetherflow"
        )
    }

    func testDetectUsesConfiguredProjectNameFromAetherflowConfig() throws {
        let tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: tempDirectory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: tempDirectory) }

        let configURL = tempDirectory.appendingPathComponent(".aetherflow.yaml")
        try """
        project: control-room
        """.write(to: configURL, atomically: true, encoding: .utf8)

        let context = ShellBootstrapContext.detect(
            environment: [:],
            currentDirectoryPath: tempDirectory.path
        )

        XCTAssertEqual(context.projectName, "control-room")
        XCTAssertEqual(context.daemonURL, ShellBootstrapContext.defaultManualDaemonURL())
        XCTAssertTrue(context.daemonTargetReason.contains("global manual daemon endpoint"))
    }

    func testDetectUsesConfiguredListenAddrFromAetherflowConfig() throws {
        let tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: tempDirectory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: tempDirectory) }

        let configURL = tempDirectory.appendingPathComponent(".aetherflow.yaml")
        try """
        project: control-room
        listen_addr: :7099
        """.write(to: configURL, atomically: true, encoding: .utf8)

        let context = ShellBootstrapContext.detect(
            environment: [:],
            currentDirectoryPath: tempDirectory.path
        )

        XCTAssertEqual(context.daemonURL, "http://127.0.0.1:7099")
        XCTAssertTrue(context.daemonTargetReason.contains("listen_addr"))
        XCTAssertEqual(context.daemonListenAddressOverride, "127.0.0.1:7099")
    }

    func testDetectSupportsConfiguredIPv6ListenAddr() throws {
        let tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: tempDirectory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: tempDirectory) }

        let configURL = tempDirectory.appendingPathComponent(".aetherflow.yaml")
        try """
        listen_addr: "[::1]:7099"
        """.write(to: configURL, atomically: true, encoding: .utf8)

        let context = ShellBootstrapContext.detect(
            environment: [:],
            currentDirectoryPath: tempDirectory.path
        )

        XCTAssertEqual(context.daemonURL, "http://[::1]:7099")
        XCTAssertEqual(context.daemonListenAddressOverride, "[::1]:7099")
    }

    func testDetectIgnoresNonLoopbackEnvironmentDaemonURL() {
        let context = ShellBootstrapContext.detect(
            environment: [
                "AETHERFLOW_PROJECT": "control-room",
                "AETHERFLOW_DAEMON_URL": "http://example.com:7070",
            ],
            currentDirectoryPath: "/tmp/control-room"
        )

        XCTAssertEqual(context.daemonURL, ShellBootstrapContext.defaultManualDaemonURL())
        XCTAssertTrue(context.daemonTargetReason.contains("global manual daemon endpoint"))
    }

    func testDetectDefaultsToGlobalManualDaemonWithoutWorkspaceConfig() {
        let context = ShellBootstrapContext.detect(
            environment: [:],
            currentDirectoryPath: "/tmp/control-room"
        )

        XCTAssertEqual(context.projectName, "control-room")
        XCTAssertEqual(context.daemonURL, ShellBootstrapContext.defaultManualDaemonURL())
        XCTAssertTrue(context.daemonTargetReason.contains("global manual daemon endpoint"))
        XCTAssertEqual(context.daemonListenAddressOverride, "127.0.0.1:7070")
    }

    @MainActor
    func testNavigationStoreResetsSelectionWhenSectionChanges() {
        let store = NavigationStore()

        let originalCardID = store.selectedCard.id
        store.select(section: .diagnostics)

        XCTAssertNotEqual(store.selectedCard.id, originalCardID)
        XCTAssertEqual(store.selectedCard.id, ShellSection.diagnostics.cards[0].id)
    }

    @MainActor
    func testWindowSceneIdentifierIsStable() {
        XCTAssertEqual(SceneID.controlCenter, "control-center")
    }
}
