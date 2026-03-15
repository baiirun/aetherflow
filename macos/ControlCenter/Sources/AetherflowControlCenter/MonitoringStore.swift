import Foundation

private let monitoringANSIEscapeRegex = try! NSRegularExpression(pattern: "\u{001B}\\[[0-9;]*[ -/]*[@-~]")

enum MonitoringConnectionPhase: String, Equatable {
    case connecting
    case connected
    case reconnecting
    case disconnected
    case failed
}

enum MonitoringWorkloadKind: String, Equatable, Sendable {
    case poolAgent
    case spawn
}

struct MonitoringWorkloadSummary: Identifiable, Equatable, Sendable {
    let id: String
    let kind: MonitoringWorkloadKind
    let role: String
    let workRef: String
    let title: String
    let subtitle: String
    let sessionID: String
    let lifecycleState: String
    let pid: Int
    let attentionNeeded: Bool
    let spawnedAt: Date
    let lastActivityAt: Date?
}

struct MonitoringQueueItem: Identifiable, Equatable, Sendable {
    let id: String
    let priority: Int
    let title: String
}

struct MonitoringSelectionDetail: Equatable, Sendable {
    let workloadID: String
    let session: DaemonSessionMetadataPayload
    let agent: DaemonAgentStatusPayload
    let toolCalls: [DaemonToolCallPayload]
    let eventLines: [String]
    let lastEventTimestamp: Int64
    let errors: [String]
    let isLive: Bool

    var lifecycleLabel: String {
        Self.normalizedLifecycleLabel(
            agentLifecycleState: agent.lifecycleState.nonEmptyValue,
            agentState: agent.state.nonEmptyValue,
            sessionStatus: session.status.nonEmptyValue,
            isLive: isLive
        )
    }

    var retainedLifecycleLabel: String {
        Self.normalizedLifecycleLabel(
            agentLifecycleState: agent.lifecycleState.nonEmptyValue,
            agentState: agent.state.nonEmptyValue,
            sessionStatus: session.status.nonEmptyValue,
            isLive: false
        )
    }

    static func isTerminalLifecycle(_ value: String) -> Bool {
        switch value.lowercased() {
        case "complete", "completed", "crashed", "error", "errored", "exited", "failed", "idle":
            return true
        default:
            return false
        }
    }

    private static func normalizedLifecycleLabel(
        agentLifecycleState: String?,
        agentState: String?,
        sessionStatus: String?,
        isLive: Bool
    ) -> String {
        let candidate = agentLifecycleState ?? agentState ?? sessionStatus ?? (isLive ? "unknown" : "exited")
        if isLive || isTerminalLifecycle(candidate) {
            return candidate
        }
        return "exited"
    }
}

struct MonitoringSnapshot: Equatable, Sendable {
    let phase: MonitoringConnectionPhase
    let project: String
    let poolMode: String
    let spawnPolicy: String
    let workloads: [MonitoringWorkloadSummary]
    let queue: [MonitoringQueueItem]
    let selectedWorkloadID: String?
    let selectedDetail: MonitoringSelectionDetail?
    let note: String
    let lastError: String?
    let updatedAt: Date

    static func initial(context: ShellBootstrapContext) -> Self {
        Self(
            phase: .connecting,
            project: context.projectName,
            poolMode: "",
            spawnPolicy: "",
            workloads: [],
            queue: [],
            selectedWorkloadID: nil,
            selectedDetail: nil,
            note: "\(context.daemonTargetReason) Waiting for the daemon monitoring endpoint to answer.",
            lastError: nil,
            updatedAt: .now
        )
    }
}

@MainActor
final class MonitoringStore: ObservableObject {
    @Published private(set) var snapshot: MonitoringSnapshot

    private static let retainedEventLineLimit = 200
    private struct SelectionAnchor: Equatable {
        let workloadID: String
        let sessionID: String?
    }

    private let context: ShellBootstrapContext
    private let controller: DaemonControlling
    private let isDaemonAbsent: (Error) -> Bool
    private let pollIntervalNanoseconds: UInt64
    private let detailLimit: Int
    private var monitorTask: Task<Void, Never>?
    private var hasConnected = false
    private var needsAuthoritativeReload = true
    private var selectionAnchor: SelectionAnchor?

    init(
        context: ShellBootstrapContext,
        controller: DaemonControlling = DefaultDaemonController(),
        isDaemonAbsent: @escaping (Error) -> Bool = { error in
            if let urlError = error as? URLError {
                return urlError.code == .cannotConnectToHost || urlError.code == .networkConnectionLost
            }
            return false
        },
        pollIntervalNanoseconds: UInt64 = 2_000_000_000,
        detailLimit: Int = 20,
        autoStartMonitoring: Bool = true
    ) {
        self.context = context
        self.controller = controller
        self.isDaemonAbsent = isDaemonAbsent
        self.pollIntervalNanoseconds = pollIntervalNanoseconds
        self.detailLimit = detailLimit
        self.snapshot = .initial(context: context)
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

    func selectWorkload(id: String?) {
        guard snapshot.selectedWorkloadID != id else {
            return
        }
        needsAuthoritativeReload = true
        selectionAnchor = id.flatMap { workloadID in
            guard let workload = snapshot.workloads.first(where: { $0.id == workloadID }) else {
                return SelectionAnchor(workloadID: workloadID, sessionID: nil)
            }
            return SelectionAnchor(workloadID: workloadID, sessionID: workload.sessionID.nonEmptyValue)
        }
        snapshot = MonitoringSnapshot(
            phase: snapshot.phase,
            project: snapshot.project,
            poolMode: snapshot.poolMode,
            spawnPolicy: snapshot.spawnPolicy,
            workloads: snapshot.workloads,
            queue: snapshot.queue,
            selectedWorkloadID: id,
            selectedDetail: nil,
            note: snapshot.note,
            lastError: snapshot.lastError,
            updatedAt: .now
        )
    }

    func refresh() async {
        do {
            let status = try await controller.fetchStatus(daemonURL: context.daemonURL)
            let workloads = orderedWorkloads(incoming: buildWorkloads(status: status), previous: snapshot.workloads)
            let selectedWorkloadID = resolvedSelectionID(
                workloads: workloads,
                previousSelection: snapshot.selectedDetail
            )
            var selectedDetail = snapshot.selectedDetail

            let reconnected = needsAuthoritativeReload || snapshot.phase != .connected
            if let selectedWorkloadID {
                let selectedWorkloadIsLive = workloads.contains { $0.id == selectedWorkloadID }
                if !selectedWorkloadIsLive, let previous = snapshot.selectedDetail {
                    selectedDetail = retainedTerminalDetail(
                        from: previous,
                        workloadID: selectedWorkloadID
                    )
                } else {
                    selectedDetail = try await refreshSelectedDetail(
                        workloadID: selectedWorkloadID,
                        previous: reconnected ? nil : snapshot.selectedDetail,
                        isLive: selectedWorkloadIsLive
                    )
                }
            } else {
                selectedDetail = nil
            }

            snapshot = MonitoringSnapshot(
                phase: .connected,
                project: status.project.nonEmptyValue ?? context.projectName,
                poolMode: status.poolMode,
                spawnPolicy: status.spawnPolicy,
                workloads: workloads,
                queue: status.queue.map { MonitoringQueueItem(id: $0.id, priority: $0.priority, title: $0.title) },
                selectedWorkloadID: selectedWorkloadID,
                selectedDetail: selectedDetail,
                note: monitoringNote(for: status, workloadCount: workloads.count),
                lastError: status.errors.last?.nonEmptyValue,
                updatedAt: .now
            )
            selectionAnchor = selectedWorkloadID.map { workloadID in
                guard let workload = workloads.first(where: { $0.id == workloadID }) else {
                    return SelectionAnchor(
                        workloadID: workloadID,
                        sessionID: selectedDetail?.session.sessionID.nonEmptyValue
                    )
                }
                return SelectionAnchor(workloadID: workloadID, sessionID: workload.sessionID.nonEmptyValue)
            }
            hasConnected = true
            needsAuthoritativeReload = false
        } catch {
            applyConnectionFailure(error)
        }
    }

    private func refreshSelectedDetail(
        workloadID: String,
        previous: MonitoringSelectionDetail?,
        isLive: Bool
    ) async throws -> MonitoringSelectionDetail {
        let detail = try await controller.fetchAgentDetail(
            daemonURL: context.daemonURL,
            agentName: workloadID,
            limit: detailLimit
        )

        let afterTimestamp = previous?.lastEventTimestamp ?? 0
        let events = try await controller.fetchEvents(
            daemonURL: context.daemonURL,
            agentName: workloadID,
            afterTimestamp: afterTimestamp
        )

        let lines = mergedEventLines(
            previous: previous?.eventLines ?? [],
            incoming: events.lines
        )
        let lastEventTimestamp = max(previous?.lastEventTimestamp ?? 0, events.lastTS)

        return MonitoringSelectionDetail(
            workloadID: workloadID,
            session: events.session.sessionID.nonEmptyValue == nil ? detail.session : events.session,
            agent: detail.agent,
            toolCalls: detail.toolCalls,
            eventLines: lines,
            lastEventTimestamp: lastEventTimestamp,
            errors: detail.errors,
            isLive: isLive
        )
    }

    private func applyConnectionFailure(_ error: Error) {
        let absent = isDaemonAbsent(error)
        if hasConnected {
            needsAuthoritativeReload = true
        }
        snapshot = MonitoringSnapshot(
            phase: hasConnected ? .reconnecting : (absent ? .disconnected : .failed),
            project: snapshot.project,
            poolMode: snapshot.poolMode,
            spawnPolicy: snapshot.spawnPolicy,
            workloads: hasConnected ? snapshot.workloads : [],
            queue: hasConnected ? snapshot.queue : [],
            selectedWorkloadID: snapshot.selectedWorkloadID,
            selectedDetail: hasConnected ? snapshot.selectedDetail : nil,
            note: monitoringFailureNote(absent: absent, detail: error.localizedDescription),
            lastError: error.localizedDescription,
            updatedAt: .now
        )
    }

    private func buildWorkloads(status: DaemonStatusPayload) -> [MonitoringWorkloadSummary] {
        let agents = status.agents.map {
            MonitoringWorkloadSummary(
                id: $0.id,
                kind: .poolAgent,
                role: $0.role,
                workRef: $0.taskID.nonEmptyValue ?? $0.id,
                title: $0.taskTitle.nonEmptyValue ?? $0.id,
                subtitle: $0.lastLog.nonEmptyValue ?? $0.taskID.nonEmptyValue ?? "Pool agent",
                sessionID: $0.sessionID,
                lifecycleState: $0.lifecycleState.nonEmptyValue ?? $0.state,
                pid: $0.pid,
                attentionNeeded: $0.attentionNeeded,
                spawnedAt: $0.spawnTime,
                lastActivityAt: $0.lastActivityAt
            )
        }
        let spawns = status.spawns.map {
            MonitoringWorkloadSummary(
                id: $0.spawnID,
                kind: .spawn,
                role: "spawn",
                workRef: $0.spawnID,
                title: $0.prompt.nonEmptyValue ?? $0.spawnID,
                subtitle: $0.sessionID.nonEmptyValue ?? ($0.state.nonEmptyValue ?? "Spawned session"),
                sessionID: $0.sessionID,
                lifecycleState: $0.lifecycleState.nonEmptyValue ?? $0.state,
                pid: $0.pid,
                attentionNeeded: $0.attentionNeeded,
                spawnedAt: $0.spawnTime,
                lastActivityAt: $0.lastActivityAt ?? $0.exitedAt
            )
        }
        return agents + spawns
    }

    private func orderedWorkloads(
        incoming: [MonitoringWorkloadSummary],
        previous: [MonitoringWorkloadSummary]
    ) -> [MonitoringWorkloadSummary] {
        let incomingByID = Dictionary(uniqueKeysWithValues: incoming.map { ($0.id, $0) })
        let retained = previous.compactMap { incomingByID[$0.id] }
        let retainedIDs = Set(retained.map(\.id))
        let appended = incoming
            .filter { !retainedIDs.contains($0.id) }
            .sorted(by: compareNewWorkloads)
        return retained + appended
    }

    private func compareNewWorkloads(
        _ lhs: MonitoringWorkloadSummary,
        _ rhs: MonitoringWorkloadSummary
    ) -> Bool {
        if lhs.attentionNeeded != rhs.attentionNeeded {
            return lhs.attentionNeeded && !rhs.attentionNeeded
        }
        if lhs.kind != rhs.kind {
            return lhs.kind == .poolAgent
        }
        if lhs.spawnedAt != rhs.spawnedAt {
            return lhs.spawnedAt > rhs.spawnedAt
        }
        return lhs.id.localizedStandardCompare(rhs.id) == .orderedAscending
    }

    private func resolvedSelectionID(
        workloads: [MonitoringWorkloadSummary],
        previousSelection: MonitoringSelectionDetail?
    ) -> String? {
        if let sessionID = selectionAnchor?.sessionID,
           let match = workloads.first(where: { $0.sessionID == sessionID }) {
            return match.id
        }
        if let workloadID = selectionAnchor?.workloadID,
           workloads.contains(where: { $0.id == workloadID }) {
            return workloadID
        }
        if shouldRetainSelection(workloads: workloads, previousSelection: previousSelection),
           let workloadID = selectionAnchor?.workloadID {
            return workloadID
        }
        guard let preferred = workloads.first else {
            return nil
        }
        return preferred.id
    }

    private func shouldRetainSelection(
        workloads: [MonitoringWorkloadSummary],
        previousSelection: MonitoringSelectionDetail?
    ) -> Bool {
        guard let previousSelection,
              let anchoredWorkloadID = selectionAnchor?.workloadID,
              anchoredWorkloadID == previousSelection.workloadID
        else {
            return false
        }
        let previousSessionID = previousSelection.session.sessionID.nonEmptyValue
        if let anchoredSessionID = selectionAnchor?.sessionID,
           anchoredSessionID != previousSessionID {
            return false
        }
        return !workloads.contains(where: { $0.id == anchoredWorkloadID })
    }

    private func mergedEventLines(previous: [String], incoming: [String]) -> [String] {
        let sanitizedIncoming = incoming.compactMap { line in
            line.strippingANSIEscapeCodes.nonEmptyValue
        }
        let merged = previous + sanitizedIncoming
        guard merged.count > Self.retainedEventLineLimit else {
            return merged
        }
        return Array(merged.suffix(Self.retainedEventLineLimit))
    }

    private func retainedTerminalDetail(
        from previous: MonitoringSelectionDetail,
        workloadID: String
    ) -> MonitoringSelectionDetail {
        assert(previous.workloadID == workloadID, "retained detail must match the selected workload")
        let lifecycleState = previous.retainedLifecycleLabel
        assert(
            MonitoringSelectionDetail.isTerminalLifecycle(lifecycleState),
            "retained detail must normalize to a terminal lifecycle"
        )
        let lastSeenAt = previous.agent.lastActivityAt
            ?? previous.session.lastSeenAt
            ?? previous.session.updatedAt

        return MonitoringSelectionDetail(
            workloadID: workloadID,
            session: DaemonSessionMetadataPayload(
                serverRef: previous.session.serverRef,
                sessionID: previous.session.sessionID,
                directory: previous.session.directory,
                project: previous.session.project,
                originType: previous.session.originType,
                workRef: previous.session.workRef,
                agentID: previous.session.agentID,
                status: lifecycleState,
                createdAt: previous.session.createdAt,
                lastSeenAt: lastSeenAt,
                updatedAt: previous.session.updatedAt,
                attachable: previous.session.attachable
            ),
            agent: DaemonAgentStatusPayload(
                id: previous.agent.id,
                taskID: previous.agent.taskID,
                role: previous.agent.role,
                pid: previous.agent.pid,
                spawnTime: previous.agent.spawnTime,
                taskTitle: previous.agent.taskTitle,
                lastLog: previous.agent.lastLog,
                sessionID: previous.agent.sessionID,
                state: lifecycleState,
                lifecycleState: lifecycleState,
                lastActivityAt: lastSeenAt,
                attentionNeeded: false
            ),
            toolCalls: previous.toolCalls,
            eventLines: previous.eventLines,
            lastEventTimestamp: previous.lastEventTimestamp,
            errors: previous.errors,
            isLive: false
        )
    }

    private func monitoringNote(for status: DaemonStatusPayload, workloadCount: Int) -> String {
        var parts = ["Monitoring connected for \(status.project.nonEmptyValue ?? context.projectName) at \(context.daemonURL)."]
        if let poolMode = status.poolMode.nonEmptyValue {
            parts.append("Pool mode: \(poolMode).")
        }
        if let spawnPolicy = status.spawnPolicy.nonEmptyValue {
            parts.append("Spawn policy: \(spawnPolicy).")
            if spawnPolicy != "manual" {
                parts.append("The app target is not serving a manual daemon.")
            }
        }
        parts.append("Visible workloads: \(workloadCount).")
        return parts.joined(separator: " ")
    }

    private func monitoringFailureNote(absent: Bool, detail: String) -> String {
        if absent {
            return hasConnected
                ? "Monitoring lost the daemon and is waiting to reconnect. \(detail)"
                : "Monitoring daemon is not reachable yet. \(detail)"
        }
        return hasConnected
            ? "Monitoring probe failed after a prior connection. \(detail)"
            : "Monitoring probe failed before the first successful connection. \(detail)"
    }
}

private extension String {
    var nonEmptyValue: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    var strippingANSIEscapeCodes: String {
        let range = NSRange(startIndex..<endIndex, in: self)
        return monitoringANSIEscapeRegex.stringByReplacingMatches(in: self, range: range, withTemplate: "")
    }
}
