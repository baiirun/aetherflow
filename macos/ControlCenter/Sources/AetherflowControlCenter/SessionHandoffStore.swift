import Foundation

protocol SessionHandoffLaunching: Sendable {
    func launch(session: DaemonSessionMetadataPayload, transport: TransportSnapshot) async throws
}

extension SessionHandoffLauncher: SessionHandoffLaunching {}

@MainActor
final class SessionHandoffStore: ObservableObject {
    @Published private var snapshot: SessionHandoffSnapshot?

    private let launcher: SessionHandoffLaunching

    init(launcher: SessionHandoffLaunching = SessionHandoffLauncher()) {
        self.launcher = launcher
    }

    func phase(for detail: MonitoringSelectionDetail) -> SessionHandoffPhase {
        let selectionKey = Self.selectionKey(for: detail)
        guard snapshot?.selectionKey == selectionKey else {
            return .idle
        }
        return snapshot?.phase ?? .idle
    }

    func requestLaunch(detail: MonitoringSelectionDetail, transport: TransportSnapshot) {
        let selectionKey = Self.selectionKey(for: detail)
        if snapshot?.selectionKey == selectionKey, snapshot?.phase == .launching {
            return
        }

        snapshot = SessionHandoffSnapshot(selectionKey: selectionKey, phase: .launching)

        Task { [weak self] in
            guard let self else {
                return
            }
            do {
                try await launcher.launch(session: detail.session, transport: transport)
                let sessionLabel = {
                    let trimmed = detail.session.sessionID.trimmingCharacters(in: .whitespacesAndNewlines)
                    return trimmed.isEmpty ? "the selected session" : trimmed
                }()
                self.apply(
                    phase: .success("Opened Opencode in Terminal for \(sessionLabel)."),
                    selectionKey: selectionKey
                )
            } catch {
                self.apply(phase: .failure(error.localizedDescription), selectionKey: selectionKey)
            }
        }
    }

    private func apply(phase: SessionHandoffPhase, selectionKey: String) {
        guard snapshot?.selectionKey == selectionKey else {
            return
        }
        snapshot = SessionHandoffSnapshot(selectionKey: selectionKey, phase: phase)
    }

    private static func selectionKey(for detail: MonitoringSelectionDetail) -> String {
        "\(detail.workloadID)::\(detail.session.sessionID)"
    }
}
