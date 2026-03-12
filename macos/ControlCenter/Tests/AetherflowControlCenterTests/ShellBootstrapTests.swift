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
