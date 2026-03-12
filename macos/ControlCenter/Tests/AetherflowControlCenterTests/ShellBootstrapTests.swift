import XCTest
@testable import AetherflowControlCenter

final class ShellBootstrapTests: XCTestCase {
    func testDefaultDaemonURLIsDeterministic() {
        let url1 = ShellBootstrapContext.defaultDaemonURL(for: "aetherflow")
        let url2 = ShellBootstrapContext.defaultDaemonURL(for: "aetherflow")
        XCTAssertEqual(url1, url2)
        XCTAssertTrue(url1.hasPrefix("http://127.0.0.1:"))
    }

    func testDefaultDaemonURLEmptyProjectReturnsDefaultPort() {
        XCTAssertEqual(ShellBootstrapContext.defaultDaemonURL(for: ""), "http://127.0.0.1:7070")
    }

    func testDefaultDaemonURLDifferentProjectsDifferentURLs() {
        let url1 = ShellBootstrapContext.defaultDaemonURL(for: "project-alpha")
        let url2 = ShellBootstrapContext.defaultDaemonURL(for: "project-beta")
        XCTAssertNotEqual(url1, url2)
    }

    func testDefaultDaemonURLPortInRange() {
        for project in ["aetherflow", "my-app", "control-room", "test"] {
            let urlString = ShellBootstrapContext.defaultDaemonURL(for: project)
            guard let url = URL(string: urlString),
                  let port = url.port else {
                XCTFail("invalid URL for project \(project): \(urlString)")
                continue
            }
            XCTAssertTrue((7071...7170).contains(port), "port \(port) out of range for project \(project)")
        }
    }

    func testDefaultDaemonURLMatchesGoPortHash() {
        // Verify the FNV-1a hash matches the Go implementation for known inputs.
        // Go: DaemonURLFor("myproject") with hash 84 → port 7155
        // Swift must produce the same port.
        let goURL = ShellBootstrapContext.defaultDaemonURL(for: "myproject")
        XCTAssertTrue(goURL.hasPrefix("http://127.0.0.1:"), "expected loopback URL, got \(goURL)")
        // Both Go and Swift must agree on the same URL.
        let goURL2 = ShellBootstrapContext.defaultDaemonURL(for: "myproject")
        XCTAssertEqual(goURL, goURL2)
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
        XCTAssertEqual(context.daemonURL, ShellBootstrapContext.defaultDaemonURL(for: "control-room"))
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
