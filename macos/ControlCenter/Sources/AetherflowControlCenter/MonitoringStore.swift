import Foundation

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
    let title: String
    let subtitle: String
    let sessionID: String
    let lifecycleState: String
    let attentionNeeded: Bool
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
    let toolCalls: [DaemonToolCallPayload]
    let eventLines: [String]
    let lastEventTimestamp: Int64
    let errors: [String]
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
            note: "Waiting for the daemon monitoring endpoint to answer.",
            lastError: nil,
            updatedAt: .now
        )
    }
}

@MainActor
final class MonitoringStore: ObservableObject {
    @Published private(set) var snapshot: MonitoringSnapshot

    private let context: ShellBootstrapContext
    private let controller: DaemonControlling
    private let isDaemonAbsent: (Error) -> Bool
    private let pollIntervalNanoseconds: UInt64
    private let detailLimit: Int
    private var monitorTask: Task<Void, Never>?
    private var hasConnected = false
    private var needsAuthoritativeReload = true

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
            let workloads = buildWorkloads(status: status)
            let selectedWorkloadID = resolvedSelectionID(workloads: workloads, preferredID: snapshot.selectedWorkloadID)
            var selectedDetail = snapshot.selectedDetail

            let reconnected = needsAuthoritativeReload || snapshot.phase != .connected
            if let selectedWorkloadID {
                selectedDetail = try await refreshSelectedDetail(
                    workloadID: selectedWorkloadID,
                    previous: reconnected ? nil : snapshot.selectedDetail
                )
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
            hasConnected = true
            needsAuthoritativeReload = false
        } catch {
            applyConnectionFailure(error)
        }
    }

    private func refreshSelectedDetail(
        workloadID: String,
        previous: MonitoringSelectionDetail?
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

        let lines: [String]
        let lastEventTimestamp: Int64
        if previous == nil {
            lines = events.lines
            lastEventTimestamp = events.lastTS
        } else {
            lines = previous!.eventLines + events.lines
            lastEventTimestamp = max(previous!.lastEventTimestamp, events.lastTS)
        }

        return MonitoringSelectionDetail(
            workloadID: workloadID,
            session: events.session.sessionID.nonEmptyValue == nil ? detail.session : events.session,
            toolCalls: detail.toolCalls,
            eventLines: lines,
            lastEventTimestamp: lastEventTimestamp,
            errors: detail.errors
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
                title: $0.taskTitle.nonEmptyValue ?? $0.id,
                subtitle: $0.lastLog.nonEmptyValue ?? $0.taskID.nonEmptyValue ?? "Pool agent",
                sessionID: $0.sessionID,
                lifecycleState: $0.lifecycleState.nonEmptyValue ?? $0.state,
                attentionNeeded: $0.attentionNeeded,
                lastActivityAt: $0.lastActivityAt
            )
        }
        let spawns = status.spawns.map {
            MonitoringWorkloadSummary(
                id: $0.spawnID,
                kind: .spawn,
                role: "spawn",
                title: $0.prompt.nonEmptyValue ?? $0.spawnID,
                subtitle: $0.state.nonEmptyValue ?? "Spawned session",
                sessionID: $0.sessionID,
                lifecycleState: $0.lifecycleState.nonEmptyValue ?? $0.state,
                attentionNeeded: $0.attentionNeeded,
                lastActivityAt: $0.lastActivityAt ?? $0.exitedAt
            )
        }
        return agents + spawns
    }

    private func resolvedSelectionID(
        workloads: [MonitoringWorkloadSummary],
        preferredID: String?
    ) -> String? {
        if let preferredID, workloads.contains(where: { $0.id == preferredID }) {
            return preferredID
        }
        return workloads.first?.id
    }

    private func monitoringNote(for status: DaemonStatusPayload, workloadCount: Int) -> String {
        var parts = ["Monitoring connected for \(status.project.nonEmptyValue ?? context.projectName)."]
        if let poolMode = status.poolMode.nonEmptyValue {
            parts.append("Pool mode: \(poolMode).")
        }
        if let spawnPolicy = status.spawnPolicy.nonEmptyValue {
            parts.append("Spawn policy: \(spawnPolicy).")
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
}
