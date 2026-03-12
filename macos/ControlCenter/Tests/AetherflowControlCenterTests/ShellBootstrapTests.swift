import XCTest
@testable import AetherflowControlCenter

final class ShellBootstrapTests: XCTestCase {
    func testDefaultSocketPathUsesLastPathComponent() {
        XCTAssertEqual(
            ShellBootstrapContext.defaultSocketPath(for: "../../workspace/aetherflow"),
            "/tmp/aetherd-aetherflow.sock"
        )
    }

    func testDetectUsesEnvironmentOverrides() {
        let context = ShellBootstrapContext.detect(
            environment: [
                "AETHERFLOW_PROJECT": "control-room",
                "AETHERFLOW_WORKING_DIRECTORY": "/tmp/control-room",
                "AETHERFLOW_SOCKET_PATH": "/tmp/custom.sock",
            ],
            currentDirectoryPath: "/ignored"
        )

        XCTAssertEqual(context.projectName, "control-room")
        XCTAssertEqual(context.workingDirectory, "/tmp/control-room")
        XCTAssertEqual(context.socketPath, "/tmp/custom.sock")
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
        XCTAssertEqual(context.socketPath, "/tmp/aetherd-control-room.sock")
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
