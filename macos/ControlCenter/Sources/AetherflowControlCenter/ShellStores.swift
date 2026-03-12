import Foundation
import SwiftUI

@MainActor
final class TransportStore: ObservableObject {
    @Published private(set) var snapshot: TransportSnapshot

    init(context: ShellBootstrapContext) {
        self.snapshot = TransportSnapshot(
            phase: .primed,
            projectName: context.projectName,
            workingDirectory: context.workingDirectory,
            daemonURL: context.daemonURL,
            cliPath: context.cliPath,
            note: "Shell bootstrap is resolved. Waiting for the first lifecycle probe."
        )
    }

    func updatePhase(_ phase: TransportPhase, note: String) {
        snapshot = TransportSnapshot(
            phase: phase,
            projectName: snapshot.projectName,
            workingDirectory: snapshot.workingDirectory,
            daemonURL: snapshot.daemonURL,
            cliPath: snapshot.cliPath,
            note: note
        )
    }
}

@MainActor
final class DaemonLifecycleStore: ObservableObject {
    @Published private(set) var snapshot = DaemonLifecycleSnapshot(
        phase: .stopped,
        activeSessions: 0,
        activeSessionIDs: [],
        serverURL: nil,
        spawnPolicy: nil,
        statusCopy: "Shell is ready. Start the daemon to begin native monitoring.",
        lastError: nil,
        updatedAt: .now
    )
    @Published private(set) var diagnostics: [LifecycleDiagnostic] = []
    @Published private(set) var banner: LifecycleBanner?
    @Published private(set) var actionInFlight: LifecycleAction?
    @Published var pendingStopConfirmation: StopConfirmationRequest?

    private let context: ShellBootstrapContext
    private let transportStore: TransportStore
    private let controller: DaemonControlling
    /// Returns true when the error indicates the daemon is not running
    /// (e.g. connection refused). False means it is running but the probe failed.
    private let isDaemonAbsent: (Error) -> Bool
    private let pollIntervalNanoseconds: UInt64
    private let startupTimeout: TimeInterval
    private var monitorTask: Task<Void, Never>?
    private var pendingAction: LifecycleAction?
    private var startupDeadline: Date?
    private var lastTransportPhase: TransportPhase?

    init(
        context: ShellBootstrapContext,
        transportStore: TransportStore,
        controller: DaemonControlling = DefaultDaemonController(),
        isDaemonAbsent: @escaping (Error) -> Bool = { error in
            if let urlError = error as? URLError {
                return urlError.code == .cannotConnectToHost || urlError.code == .networkConnectionLost
            }
            return false
        },
        pollIntervalNanoseconds: UInt64 = 2_000_000_000,
        startupTimeout: TimeInterval = 10,
        autoStartMonitoring: Bool = true
    ) {
        self.context = context
        self.transportStore = transportStore
        self.controller = controller
        self.isDaemonAbsent = isDaemonAbsent
        self.pollIntervalNanoseconds = pollIntervalNanoseconds
        self.startupTimeout = startupTimeout
        if autoStartMonitoring {
            beginMonitoring()
        }
    }

    deinit {
        monitorTask?.cancel()
    }

    func beginMonitoring() {
        guard monitorTask == nil else {
            return
        }
        monitorTask = Task { [weak self] in
            guard let self else {
                return
            }
            await self.refresh()
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: self.pollIntervalNanoseconds)
                await self.refresh()
            }
        }
    }

    func requestStart() {
        guard actionInFlight == nil else {
            return
        }
        let controller = self.controller
        let context = self.context
        actionInFlight = .starting
        banner = nil
        appendDiagnostic(
            title: "Start requested",
            detail: "Launching daemon from \(context.cliPath) in \(context.workingDirectory).",
            tone: .info
        )

        Task {
            do {
                let receipt = try await controller.requestStart(context: context)
                applyStartReceipt(receipt)
                await refresh()
            } catch {
                applyActionFailure(
                    action: .starting,
                    title: "Start failed",
                    detail: error.localizedDescription
                )
            }
        }
    }

    func requestStop() {
        pendingStopConfirmation = nil
        sendStop(force: false)
    }

    func confirmStop() {
        pendingStopConfirmation = nil
        sendStop(force: true)
    }

    func dismissStopConfirmation() {
        pendingStopConfirmation = nil
    }

    func refresh() async {
        let controller = self.controller
        let daemonURL = context.daemonURL
        do {
            let lifecycle = try await controller.fetchLifecycle(daemonURL: daemonURL)
            applyLifecycle(lifecycle)
        } catch {
            applyUnavailableState(error: error)
        }
    }

    private func sendStop(force: Bool) {
        guard actionInFlight == nil else {
            return
        }
        let controller = self.controller
        let daemonURL = context.daemonURL
        actionInFlight = .stopping
        banner = nil
        appendDiagnostic(
            title: force ? "Forced stop requested" : "Stop requested",
            detail: "Requesting daemon shutdown at \(context.daemonURL).",
            tone: .info
        )

        Task {
            do {
                let response = try await controller.requestStop(daemonURL: daemonURL, force: force)
                applyStopResponse(response)
                await refresh()
            } catch {
                applyActionFailure(
                    action: .stopping,
                    title: "Stop failed",
                    detail: error.localizedDescription
                )
            }
        }
    }

    private func applyStartReceipt(_ receipt: DaemonStartReceipt) {
        pendingAction = .starting
        startupDeadline = .now.addingTimeInterval(startupTimeout)
        actionInFlight = nil
        snapshot = DaemonLifecycleSnapshot(
            phase: .starting,
            activeSessions: 0,
            activeSessionIDs: [],
            serverURL: snapshot.serverURL,
            spawnPolicy: snapshot.spawnPolicy,
            statusCopy: "Daemon start requested. Waiting for the HTTP endpoint to respond.",
            lastError: nil,
            updatedAt: .now
        )
        banner = LifecycleBanner(tone: .info, title: "Start requested", message: receipt.message)
        transportStore.updatePhase(.unreachable, note: "Start requested. Waiting for the daemon to become reachable.")
    }

    private func applyStopResponse(_ response: DaemonStopResponse) {
        actionInFlight = nil
        applyLifecyclePayload(response.status, overrideMessage: response.message)

        switch response.outcome {
        case "stopping", "stopped":
            pendingAction = .stopping
            startupDeadline = nil
            banner = LifecycleBanner(tone: .info, title: "Stop acknowledged", message: response.message)
            appendDiagnostic(title: "Stop acknowledged", detail: response.message, tone: .info)
        case "refused":
            pendingAction = nil
            banner = LifecycleBanner(tone: .warning, title: "Stop refused", message: response.message)
            appendDiagnostic(title: "Stop refused", detail: response.message, tone: .warning)
            if response.status.activeSessionCount > 0 {
                pendingStopConfirmation = StopConfirmationRequest(
                    activeSessions: response.status.activeSessionCount,
                    message: response.message
                )
            }
        default:
            pendingAction = nil
            banner = LifecycleBanner(tone: .error, title: "Stop failed", message: response.message)
            appendDiagnostic(title: "Stop failed", detail: response.message, tone: .error)
        }
    }

    private func applyLifecycle(_ lifecycle: DaemonLifecyclePayload) {
        applyLifecyclePayload(lifecycle, overrideMessage: nil)

        let transportNote = transportNote(for: lifecycle)
        transportStore.updatePhase(.connected, note: transportNote)
        if lastTransportPhase != .connected {
            appendDiagnostic(title: "Daemon reachable", detail: transportNote, tone: .success)
        }
        lastTransportPhase = .connected

        if pendingAction == .starting && snapshot.phase == .running {
            pendingAction = nil
            startupDeadline = nil
            banner = LifecycleBanner(tone: .success, title: "Daemon running", message: "Lifecycle probe connected after start.")
            appendDiagnostic(title: "Daemon running", detail: "The daemon accepted connections and reported a running state.", tone: .success)
        }
        if pendingAction == .stopping && (snapshot.phase == .stopping || snapshot.phase == .stopped) {
            startupDeadline = nil
            banner = LifecycleBanner(tone: .info, title: "Daemon stopping", message: "The daemon accepted the stop request and is winding down.")
        }
    }

    private func applyUnavailableState(error: Error) {
        let daemonAbsent = isDaemonAbsent(error)
        let detail = error.localizedDescription
        if pendingAction == .starting, startupTimedOut() {
            pendingAction = nil
            startupDeadline = nil
            banner = LifecycleBanner(tone: .error, title: "Start timed out", message: "The daemon never became reachable after the start request.")
            appendDiagnostic(title: "Start timed out", detail: "The daemon did not respond within \(Int(startupTimeout)) seconds.", tone: .error)
            snapshot = DaemonLifecycleSnapshot(
                phase: .failed,
                activeSessions: 0,
                activeSessionIDs: [],
                serverURL: snapshot.serverURL,
                spawnPolicy: snapshot.spawnPolicy,
                statusCopy: "Start requested, but the daemon never became reachable.",
                lastError: detail,
                updatedAt: .now
            )
            transportStore.updatePhase(.unreachable, note: "The daemon start request timed out before becoming reachable.")
            lastTransportPhase = .unreachable
            return
        }

        if !daemonAbsent {
            let note = "The daemon is running but the lifecycle probe failed. \(detail)"
            transportStore.updatePhase(.unreachable, note: note)
            lastTransportPhase = .unreachable

            let phase: DaemonLifecyclePhase = pendingAction == .starting ? .starting : (pendingAction == .stopping ? .stopping : .failed)
            snapshot = DaemonLifecycleSnapshot(
                phase: phase,
                activeSessions: snapshot.activeSessions,
                activeSessionIDs: snapshot.activeSessionIDs,
                serverURL: snapshot.serverURL,
                spawnPolicy: snapshot.spawnPolicy,
                statusCopy: phase == .starting ? "Daemon start requested. Waiting for the process to accept requests." : "Transport failed while the daemon is still running.",
                lastError: detail,
                updatedAt: .now
            )
            banner = LifecycleBanner(tone: .error, title: "Lifecycle probe failed", message: detail)
            return
        }

        let phase: DaemonLifecyclePhase = pendingAction == .starting ? .starting : .stopped
        let note = pendingAction == .starting
            ? "Start requested. Waiting for the daemon to start."
            : "Daemon is not running. Start the daemon to begin monitoring."
        transportStore.updatePhase(.unreachable, note: note)
        if lastTransportPhase == .connected && pendingAction == .stopping {
            appendDiagnostic(title: "Daemon disconnected", detail: "The daemon stopped responding after the stop request.", tone: .success)
            banner = LifecycleBanner(tone: .success, title: "Daemon stopped", message: "The daemon stopped responding to lifecycle probes.")
            pendingAction = nil
            startupDeadline = nil
        }
        lastTransportPhase = .unreachable

        snapshot = DaemonLifecycleSnapshot(
            phase: phase,
            activeSessions: 0,
            activeSessionIDs: [],
            serverURL: snapshot.serverURL,
            spawnPolicy: snapshot.spawnPolicy,
            statusCopy: phase == .starting ? "Daemon start requested. Waiting for the daemon to finish booting." : "Daemon is not running.",
            lastError: phase == .starting ? nil : nil,
            updatedAt: .now
        )
    }

    private func applyActionFailure(action: LifecycleAction, title: String, detail: String) {
        pendingAction = nil
        startupDeadline = nil
        actionInFlight = nil
        banner = LifecycleBanner(tone: .error, title: title, message: detail)
        appendDiagnostic(title: title, detail: detail, tone: .error)
        snapshot = DaemonLifecycleSnapshot(
            phase: .failed,
            activeSessions: snapshot.activeSessions,
            activeSessionIDs: snapshot.activeSessionIDs,
            serverURL: snapshot.serverURL,
            spawnPolicy: snapshot.spawnPolicy,
            statusCopy: "\(action.rawValue) failed.",
            lastError: detail,
            updatedAt: .now
        )
    }

    private func applyLifecyclePayload(_ lifecycle: DaemonLifecyclePayload, overrideMessage: String?) {
        snapshot = DaemonLifecycleSnapshot(
            phase: mapLifecyclePhase(lifecycle.state),
            activeSessions: lifecycle.activeSessionCount,
            activeSessionIDs: lifecycle.activeSessionIDs,
            serverURL: lifecycle.serverURL.nonEmptyValue,
            spawnPolicy: lifecycle.spawnPolicy.nonEmptyValue,
            statusCopy: lifecycleSummary(for: lifecycle, overrideMessage: overrideMessage),
            lastError: lifecycle.lastError.nonEmptyValue,
            updatedAt: lifecycle.updatedAt
        )
    }

    private func lifecycleSummary(for lifecycle: DaemonLifecyclePayload, overrideMessage: String?) -> String {
        if let overrideMessage, !overrideMessage.isEmpty {
            return overrideMessage
        }

        switch mapLifecyclePhase(lifecycle.state) {
        case .starting:
            return "Daemon is starting. Waiting for the control plane to finish booting."
        case .running:
            if lifecycle.activeSessionCount > 0 {
                return "Daemon is running with \(lifecycle.activeSessionCount) attached session(s)."
            }
            return "Daemon is running and ready for new work."
        case .stopping:
            return "Daemon is stopping. Reconnect behavior will settle once the daemon exits."
        case .stopped:
            return "Daemon is stopped."
        case .failed:
            return lifecycle.lastError.nonEmptyValue ?? "Daemon reported a failed lifecycle state."
        }
    }

    private func transportNote(for lifecycle: DaemonLifecyclePayload) -> String {
        var parts = ["Lifecycle probe succeeded for \(lifecycle.project.nonEmptyValue ?? context.projectName)."]
        if let spawnPolicy = lifecycle.spawnPolicy.nonEmptyValue {
            parts.append("Spawn policy: \(spawnPolicy).")
        }
        if lifecycle.activeSessionCount > 0 {
            parts.append("Attached sessions: \(lifecycle.activeSessionCount).")
        }
        return parts.joined(separator: " ")
    }

    private func appendDiagnostic(title: String, detail: String, tone: LifecycleBannerTone) {
        diagnostics.insert(
            LifecycleDiagnostic(timestamp: .now, title: title, detail: detail, tone: tone),
            at: 0
        )
        if diagnostics.count > 8 {
            diagnostics.removeLast(diagnostics.count - 8)
        }
    }

    private func mapLifecyclePhase(_ state: String) -> DaemonLifecyclePhase {
        switch state {
        case "starting":
            return .starting
        case "running":
            return .running
        case "stopping":
            return .stopping
        case "failed":
            return .failed
        default:
            return .stopped
        }
    }

    private func startupTimedOut(now: Date = .now) -> Bool {
        guard let startupDeadline else {
            return false
        }
        return now >= startupDeadline
    }
}

private extension String {
    var nonEmptyValue: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}

@MainActor
final class NavigationStore: ObservableObject {
    @Published private(set) var selectedSection: ShellSection
    @Published private(set) var selectedCardID: String

    init() {
        let defaultSection = ShellSection.sessions
        self.selectedSection = defaultSection
        self.selectedCardID = Self.firstCardID(in: defaultSection)
    }

    var cards: [ShellCard] {
        selectedSection.cards
    }

    var selectedCard: ShellCard {
        cards.first(where: { $0.id == selectedCardID }) ?? Self.firstCard(in: selectedSection)
    }

    var selectedSectionBinding: Binding<ShellSection?> {
        Binding(
            get: { self.selectedSection },
            set: { newValue in
                guard let newValue else {
                    return
                }
                self.select(section: newValue)
            }
        )
    }

    var selectedCardBinding: Binding<String?> {
        Binding(
            get: { self.selectedCardID },
            set: { newValue in
                guard let newValue else {
                    return
                }
                self.select(cardID: newValue)
            }
        )
    }

    func select(section: ShellSection) {
        selectedSection = section
        if !cards.contains(where: { $0.id == selectedCardID }) {
            selectedCardID = Self.firstCardID(in: section)
        }
    }

    func select(cardID: String) {
        guard cards.contains(where: { $0.id == cardID }) else {
            return
        }
        selectedCardID = cardID
    }

    private static func firstCard(in section: ShellSection) -> ShellCard {
        guard let first = section.cards.first else {
            preconditionFailure("NavigationStore requires non-empty cards for section \(section.rawValue)")
        }
        return first
    }

    private static func firstCardID(in section: ShellSection) -> String {
        firstCard(in: section).id
    }
}
