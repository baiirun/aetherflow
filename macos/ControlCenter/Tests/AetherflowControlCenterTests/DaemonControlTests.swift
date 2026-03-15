import Foundation
import XCTest
@testable import AetherflowControlCenter

final class DaemonControlTests: XCTestCase {
    func testFetchStatusConnectsToStatusEndpoint() async throws {
        let responseData = try JSONEncoder().encode(
            RPCSuccessEnvelope(
                result: DaemonStatusPayload(
                    poolSize: 1,
                    poolMode: "active",
                    project: "control-room",
                    spawnPolicy: "manual",
                    agents: [],
                    spawns: [],
                    queue: [],
                    errors: []
                )
            )
        )

        var capturedRequest: URLRequest?
        MockHTTPProtocol.handler = { request in
            capturedRequest = request
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, responseData)
        }
        defer { MockHTTPProtocol.handler = nil }

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockHTTPProtocol.self]
        let session = URLSession(configuration: config)

        let response = try await DefaultDaemonController(session: session)
            .fetchStatus(daemonURL: "http://127.0.0.1:7070")

        XCTAssertEqual(response.project, "control-room")
        XCTAssertEqual(capturedRequest?.url?.path, "/api/v1/status")
        XCTAssertEqual(capturedRequest?.httpMethod, "GET")
    }

    func testFetchLifecycleConnectsToHTTPEndpoint() async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let responseEnvelope = RPCSuccessEnvelope(
            result: DaemonLifecyclePayload(
                state: "running",
                daemonURL: "http://127.0.0.1:7070",
                project: "control-room",
                serverURL: "http://127.0.0.1:4096",
                spawnPolicy: "manual",
                activeSessionCount: 1,
                activeSessionIDs: ["sess-1"],
                lastError: "",
                updatedAt: Date.now
            )
        )
        let responseData = try encoder.encode(responseEnvelope)

        var capturedRequest: URLRequest?
        MockHTTPProtocol.handler = { request in
            capturedRequest = request
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, responseData)
        }
        defer { MockHTTPProtocol.handler = nil }

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockHTTPProtocol.self]
        let session = URLSession(configuration: config)

        let response = try await DefaultDaemonController(session: session)
            .fetchLifecycle(daemonURL: "http://127.0.0.1:7070")

        XCTAssertEqual(response.state, "running")
        XCTAssertEqual(response.project, "control-room")
        XCTAssertEqual(capturedRequest?.url?.path, "/api/v1/lifecycle")
        XCTAssertEqual(capturedRequest?.httpMethod, "GET")
    }

    func testRequestStopSendsPostToShutdownEndpoint() async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let stopResult = StopResultEnvelope(
            result: StopResponsePayload(
                outcome: "stopping",
                status: DaemonLifecyclePayload(
                    state: "stopping",
                    daemonURL: "http://127.0.0.1:7070",
                    project: "control-room",
                    serverURL: "http://127.0.0.1:4096",
                    spawnPolicy: "manual",
                    activeSessionCount: 0,
                    activeSessionIDs: [],
                    lastError: "",
                    updatedAt: Date.now
                ),
                message: "daemon stopping"
            )
        )
        let responseData = try encoder.encode(stopResult)

        var capturedRequest: URLRequest?
        MockHTTPProtocol.handler = { request in
            capturedRequest = request
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, responseData)
        }
        defer { MockHTTPProtocol.handler = nil }

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockHTTPProtocol.self]
        let session = URLSession(configuration: config)

        let response = try await DefaultDaemonController(session: session)
            .requestStop(daemonURL: "http://127.0.0.1:7070", force: false)

        XCTAssertEqual(response.outcome, "stopping")
        XCTAssertEqual(capturedRequest?.url?.path, "/api/v1/shutdown")
        XCTAssertEqual(capturedRequest?.httpMethod, "POST")
        XCTAssertNil(capturedRequest?.url?.query)
    }

    func testRequestStopWithForceAddsQueryParameter() async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let stopResult = StopResultEnvelope(
            result: StopResponsePayload(
                outcome: "stopping",
                status: DaemonLifecyclePayload(
                    state: "stopping",
                    daemonURL: "http://127.0.0.1:7070",
                    project: "control-room",
                    serverURL: "http://127.0.0.1:4096",
                    spawnPolicy: "manual",
                    activeSessionCount: 0,
                    activeSessionIDs: [],
                    lastError: "",
                    updatedAt: Date.now
                ),
                message: "daemon stopping (forced)"
            )
        )
        let responseData = try encoder.encode(stopResult)

        var capturedRequest: URLRequest?
        MockHTTPProtocol.handler = { request in
            capturedRequest = request
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, responseData)
        }
        defer { MockHTTPProtocol.handler = nil }

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockHTTPProtocol.self]
        let session = URLSession(configuration: config)

        _ = try await DefaultDaemonController(session: session)
            .requestStop(daemonURL: "http://127.0.0.1:7070", force: true)

        XCTAssertEqual(capturedRequest?.url?.query, "force=true")
    }

    func testFetchEventsIncludesAfterTimestampQuery() async throws {
        let responseData = try JSONEncoder().encode(
            RPCSuccessEnvelope(
                result: DaemonEventsPayload(
                    lines: ["session.created"],
                    sessionID: "ses-1",
                    session: .empty,
                    lastTS: 42
                )
            )
        )

        var capturedRequest: URLRequest?
        MockHTTPProtocol.handler = { request in
            capturedRequest = request
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, responseData)
        }
        defer { MockHTTPProtocol.handler = nil }

        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [MockHTTPProtocol.self]
        let session = URLSession(configuration: config)

        let response = try await DefaultDaemonController(session: session)
            .fetchEvents(daemonURL: "http://127.0.0.1:7070", agentName: "agent-1", afterTimestamp: 42)

        XCTAssertEqual(response.lastTS, 42)
        XCTAssertEqual(capturedRequest?.url?.path, "/api/v1/events")
        XCTAssertEqual(
            URLComponents(url: try XCTUnwrap(capturedRequest?.url), resolvingAgainstBaseURL: false)?.queryItems?.sorted { $0.name < $1.name },
            [
                URLQueryItem(name: "after_timestamp", value: "42"),
                URLQueryItem(name: "agent_name", value: "agent-1"),
            ]
        )
    }

    func testRequestStartUsesGlobalManualDaemonDefaults() async throws {
        let capturedInvocation = CommandInvocationBox()
        let controller = DefaultDaemonController(
            session: URLSession(configuration: .ephemeral),
            commandRunner: { invocation in
                capturedInvocation.value = invocation
                return CommandOutput(status: 0, stdout: "daemon started", stderr: "")
            }
        )

        _ = try await controller.requestStart(
            context: ShellBootstrapContext(
                projectName: "control-room",
                workingDirectory: "/tmp/control-room",
                daemonURL: "http://127.0.0.1:7070",
                cliPath: "/usr/local/bin/af"
            )
        )

        XCTAssertEqual(capturedInvocation.value?.arguments, ["daemon", "start", "--detach", "--spawn-policy", "manual", "--listen-addr", "127.0.0.1:7070"])
        XCTAssertEqual(capturedInvocation.value?.currentDirectory, "/tmp/control-room")
    }

    func testRequestStartPreservesExplicitListenAddrOverride() async throws {
        let capturedInvocation = CommandInvocationBox()
        let controller = DefaultDaemonController(
            session: URLSession(configuration: .ephemeral),
            commandRunner: { invocation in
                capturedInvocation.value = invocation
                return CommandOutput(status: 0, stdout: "daemon started", stderr: "")
            }
        )

        _ = try await controller.requestStart(
            context: ShellBootstrapContext(
                projectName: "control-room",
                workingDirectory: "/tmp/control-room",
                daemonURL: "http://127.0.0.1:7099",
                cliPath: "/usr/local/bin/af",
                daemonTargetSource: .environmentOverride,
                daemonTargetReason: "Using explicit override.",
                daemonListenAddressOverride: "127.0.0.1:7099"
            )
        )

        XCTAssertEqual(capturedInvocation.value?.arguments, ["daemon", "start", "--detach", "--spawn-policy", "manual", "--listen-addr", "127.0.0.1:7099"])
    }
}

// MARK: - Helpers

private struct RPCSuccessEnvelope<Result: Encodable>: Encodable {
    let success = true
    let result: Result
}

private typealias StopResultEnvelope = RPCSuccessEnvelope<StopResponsePayload>

private struct StopResponsePayload: Encodable {
    let outcome: String
    let status: DaemonLifecyclePayload
    let message: String
}

// MARK: - Mock URLProtocol

final class MockHTTPProtocol: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var handler: ((URLRequest) throws -> (HTTPURLResponse, Data))?

    override class func canInit(with request: URLRequest) -> Bool {
        handler != nil
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        guard let handler = Self.handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.unknown))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

private final class CommandInvocationBox: @unchecked Sendable {
    var value: CommandInvocation?
}
