import Darwin
import Foundation
import XCTest
@testable import AetherflowControlCenter

final class DaemonControlTests: XCTestCase {
    func testFetchLifecycleConnectsToLiveUnixSocket() async throws {
        let capturedRequest = LockedValue<String?>(nil)
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let server = try TestUnixSocketServer(
            responseData: try encoder.encode(
                RPCSuccessEnvelope(
                    result: DaemonLifecyclePayload(
                        state: "running",
                        socketPath: "/tmp/aetherd-control-room.sock",
                        project: "control-room",
                        serverURL: "http://127.0.0.1:4096",
                        spawnPolicy: "manual",
                        activeSessionCount: 1,
                        activeSessionIDs: ["sess-1"],
                        lastError: "",
                        updatedAt: Date.now
                    )
                )
            ) + Data([0x0A]),
            onRequest: { request in
                let envelope = try JSONDecoder().decode(TestRPCRequest.self, from: request)
                capturedRequest.set(envelope.method)
            }
        )
        defer { server.stop() }

        let response = try await DefaultDaemonController().fetchLifecycle(socketPath: server.socketPath)

        XCTAssertEqual(response.state, "running")
        XCTAssertEqual(response.project, "control-room")
        XCTAssertEqual(capturedRequest.get(), "daemon.lifecycle")
    }
}

private struct RPCSuccessEnvelope<Result: Encodable>: Encodable {
    let success = true
    let result: Result
}

private struct TestRPCRequest: Decodable {
    let method: String
}

private final class LockedValue<T>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: T

    init(_ value: T) {
        self.value = value
    }

    func set(_ newValue: T) {
        lock.lock()
        value = newValue
        lock.unlock()
    }

    func get() -> T {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

private final class TestUnixSocketServer {
    let socketPath: String

    private let listenFD: Int32
    private let onRequest: @Sendable (Data) throws -> Void
    private let responseData: Data
    private let completion = DispatchSemaphore(value: 0)

    init(
        responseData: Data,
        onRequest: @escaping @Sendable (Data) throws -> Void
    ) throws {
        self.responseData = responseData
        self.onRequest = onRequest

        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        self.socketPath = directory.appendingPathComponent("daemon.sock").path

        unlink(socketPath)
        listenFD = socket(AF_UNIX, SOCK_STREAM, 0)
        guard listenFD >= 0 else {
            throw POSIXError(.EIO)
        }

        var address = try Self.socketAddress(for: socketPath)
        let bindResult = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(listenFD, $0, Self.socketAddressLength(for: socketPath))
            }
        }
        guard bindResult == 0 else {
            close(listenFD)
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
        guard listen(listenFD, 1) == 0 else {
            close(listenFD)
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }

        DispatchQueue.global(qos: .userInitiated).async { [listenFD, responseData, onRequest, completion] in
            defer { completion.signal() }
            let clientFD = accept(listenFD, nil, nil)
            guard clientFD >= 0 else {
                return
            }
            defer { close(clientFD) }

            var requestData = Data()
            var buffer = [UInt8](repeating: 0, count: 1024)
            while true {
                let readCount = read(clientFD, &buffer, buffer.count)
                if readCount <= 0 {
                    return
                }
                requestData.append(buffer, count: readCount)
                if requestData.contains(0x0A) {
                    break
                }
            }

            let trimmedRequest = Data(requestData.prefix { $0 != 0x0A })
            try? onRequest(trimmedRequest)
            responseData.withUnsafeBytes { rawBuffer in
                _ = write(clientFD, rawBuffer.baseAddress!, rawBuffer.count)
            }
        }
    }

    func stop() {
        shutdown(listenFD, SHUT_RDWR)
        close(listenFD)
        _ = completion.wait(timeout: .now() + 1)
        unlink(socketPath)
        try? FileManager.default.removeItem(at: URL(fileURLWithPath: socketPath).deletingLastPathComponent())
    }

    private static func socketAddress(for path: String) throws -> sockaddr_un {
        var address = sockaddr_un()
#if os(macOS)
        address.sun_len = 0
#endif
        address.sun_family = sa_family_t(AF_UNIX)

        let pathBytes = Array(path.utf8CString)
        let pathOffset = MemoryLayout.offset(of: \sockaddr_un.sun_path) ?? 0
        let addressLength = pathOffset + pathBytes.count
        guard pathBytes.count <= MemoryLayout.size(ofValue: address.sun_path) else {
            throw POSIXError(.ENAMETOOLONG)
        }

#if os(macOS)
        address.sun_len = __uint8_t(addressLength)
#endif
        withUnsafeMutablePointer(to: &address) { pointer in
            let rawPointer = UnsafeMutableRawPointer(pointer).advanced(by: pathOffset)
            pathBytes.withUnsafeBytes { sourceBytes in
                rawPointer.copyMemory(from: sourceBytes.baseAddress!, byteCount: pathBytes.count)
            }
        }
        return address
    }

    private static func socketAddressLength(for path: String) -> socklen_t {
        let pathOffset = MemoryLayout.offset(of: \sockaddr_un.sun_path) ?? 0
        return socklen_t(pathOffset + path.utf8CString.count)
    }
}
