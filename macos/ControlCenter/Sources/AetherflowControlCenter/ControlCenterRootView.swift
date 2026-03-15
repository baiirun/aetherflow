import SwiftUI

private enum ShellPalette {
    static let canvasTop = Color(red: 0.96, green: 0.92, blue: 0.86)
    static let canvasBottom = Color(red: 0.89, green: 0.84, blue: 0.77)
    static let ink = Color(red: 0.12, green: 0.11, blue: 0.10)
    static let mutedInk = Color(red: 0.34, green: 0.31, blue: 0.28)
    static let ember = Color(red: 0.75, green: 0.35, blue: 0.22)
    static let moss = Color(red: 0.30, green: 0.39, blue: 0.33)
    static let brass = Color(red: 0.78, green: 0.64, blue: 0.33)
    static let panel = Color.white.opacity(0.62)
    static let panelBorder = Color.white.opacity(0.48)
    static let grid = Color.white.opacity(0.22)
}

struct ControlCenterRootView: View {
    @EnvironmentObject private var transportStore: TransportStore
    @EnvironmentObject private var lifecycleStore: DaemonLifecycleStore
    @EnvironmentObject private var monitoringStore: MonitoringStore
    @EnvironmentObject private var navigationStore: NavigationStore

    var body: some View {
        NavigationSplitView {
            SidebarColumn(selection: navigationStore.selectedSectionBinding)
        } content: {
            ContentColumn(
                selectedSection: navigationStore.selectedSection,
                cards: navigationStore.cards,
                selection: navigationStore.selectedCardBinding
            )
        } detail: {
            DetailColumn(
                selectedSection: navigationStore.selectedSection,
                selectedCard: navigationStore.selectedCard,
                transport: transportStore.snapshot,
                lifecycle: lifecycleStore.snapshot,
                monitoring: monitoringStore.snapshot
            )
        }
        .navigationSplitViewStyle(.balanced)
        .frame(minWidth: 1280, minHeight: 820)
        .background(AtmosphereBackground())
        .alert("Stop daemon?", isPresented: stopConfirmationPresented) {
            Button("Stop Now", role: .destructive) {
                lifecycleStore.confirmStop()
            }
            Button("Cancel", role: .cancel) {
                lifecycleStore.dismissStopConfirmation()
            }
        } message: {
            Text(lifecycleStore.pendingStopConfirmation?.message ?? "")
        }
        .toolbar {
            ToolbarItemGroup(placement: .navigation) {
                ToolbarRouteButton(title: "Bridge", shortcut: "1", isActive: navigationStore.selectedSection == .overview) {
                    navigationStore.select(section: .overview)
                }
                ToolbarRouteButton(title: "Sessions", shortcut: "2", isActive: navigationStore.selectedSection == .sessions) {
                    navigationStore.select(section: .sessions)
                }
                ToolbarRouteButton(title: "Queue", shortcut: "3", isActive: navigationStore.selectedSection == .queue) {
                    navigationStore.select(section: .queue)
                }
                ToolbarRouteButton(title: "Diagnostics", shortcut: "4", isActive: navigationStore.selectedSection == .diagnostics) {
                    navigationStore.select(section: .diagnostics)
                }
            }
        }
    }

    private var stopConfirmationPresented: Binding<Bool> {
        Binding(
            get: { lifecycleStore.pendingStopConfirmation != nil },
            set: { isPresented in
                if !isPresented {
                    lifecycleStore.dismissStopConfirmation()
                }
            }
        )
    }
}

private struct SidebarColumn: View {
    @Binding var selection: ShellSection?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ShellPanel {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Aetherflow")
                        .font(.system(size: 13, weight: .semibold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    Text("Control center shell")
                        .font(.system(size: 30, weight: .bold, design: .serif))
                        .foregroundStyle(ShellPalette.ink)
                    Text("Warm, dense, and operational. This shell is designed to hold long-running session work without needing structural rework.")
                        .font(.system(size: 14, weight: .medium, design: .rounded))
                        .foregroundStyle(ShellPalette.mutedInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

            List(ShellSection.allCases, selection: $selection) { section in
                ZStack {
                    RoundedRectangle(cornerRadius: 18)
                        .fill(section == selection ? ShellPalette.panel : Color.white.opacity(0.18))
                        .overlay(
                            RoundedRectangle(cornerRadius: 18)
                                .stroke(section == selection ? ShellPalette.ember.opacity(0.40) : ShellPalette.panelBorder, lineWidth: 1)
                        )
                    HStack(alignment: .top, spacing: 12) {
                        Image(systemName: section.symbolName)
                            .font(.system(size: 14, weight: .semibold))
                            .frame(width: 28, height: 28)
                            .background(
                                RoundedRectangle(cornerRadius: 10)
                                    .fill(section == selection ? ShellPalette.ember.opacity(0.18) : ShellPalette.moss.opacity(0.10))
                            )
                        VStack(alignment: .leading, spacing: 4) {
                            Text(section.title)
                                .font(.system(size: 15, weight: .semibold, design: .rounded))
                            Text(section.subtitle)
                                .font(.system(size: 12, weight: .medium, design: .rounded))
                                .foregroundStyle(ShellPalette.mutedInk)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        Spacer(minLength: 8)
                        if section == selection {
                            Text("Live")
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                                .padding(.horizontal, 8)
                                .padding(.vertical, 4)
                                .background(Capsule().fill(ShellPalette.ink))
                                .foregroundStyle(.white)
                        }
                    }
                    .padding(12)
                }
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
                .tag(section)
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
            .frame(maxHeight: .infinity)

            Spacer()

            ShellPanel {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Shell posture")
                        .font(.system(size: 12, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    Text("Three columns. Clear state seams. Menu bar presence. Enough structure for keyboard-heavy operators to stay oriented.")
                        .font(.system(size: 13, weight: .medium, design: .rounded))
                        .foregroundStyle(ShellPalette.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .padding(18)
    }
}

private struct ContentColumn: View {
    @EnvironmentObject private var monitoringStore: MonitoringStore
    let selectedSection: ShellSection
    let cards: [ShellCard]
    @Binding var selection: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ShellPanel {
                VStack(alignment: .leading, spacing: 10) {
                    Text(contentLabel)
                        .font(.system(size: 12, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    Text(contentDescription)
                        .font(.system(size: 14, weight: .medium, design: .rounded))
                        .foregroundStyle(ShellPalette.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

            switch selectedSection {
            case .sessions:
                SessionsContentColumn(
                    snapshot: monitoringStore.snapshot,
                    onSelect: monitoringStore.selectWorkload(id:)
                )
            case .queue:
                QueueContentColumn(snapshot: monitoringStore.snapshot)
            case .overview, .diagnostics:
                CardContentColumn(cards: cards, selection: $selection)
            }
        }
        .padding(18)
    }

    private var contentLabel: String {
        switch selectedSection {
        case .sessions:
            return "Live sessions"
        case .queue:
            return "Queued work"
        case .overview, .diagnostics:
            return "Operator lanes"
        }
    }

    private var contentDescription: String {
        switch selectedSection {
        case .sessions:
            return "The center lane now reflects the daemon status feed directly: pool agents, ad-hoc spawns, and reconnect-safe selection all live here."
        case .queue:
            return "Queue visibility stays inside the same shell so waiting work can be scanned without leaving the monitoring surface."
        case .overview, .diagnostics:
            return "The center lane hosts the active story of the product surface: what this section needs to show, and how it will expand when live data arrives."
        }
    }
}

private struct CardContentColumn: View {
    let cards: [ShellCard]
    @Binding var selection: String?

    var body: some View {
        List(cards, selection: $selection) { card in
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text(card.eyebrow.uppercased())
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.ember)
                    Spacer()
                    Text(card.statusLabel)
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(Capsule().fill(ShellPalette.brass.opacity(0.20)))
                        .foregroundStyle(ShellPalette.ink)
                }
                Text(card.title)
                    .font(.system(size: 22, weight: .bold, design: .serif))
                    .foregroundStyle(ShellPalette.ink)
                Text(card.summary)
                    .font(.system(size: 14, weight: .medium, design: .rounded))
                    .foregroundStyle(ShellPalette.mutedInk)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 22)
                    .fill(card.id == selection ? ShellPalette.panel : Color.white.opacity(0.22))
                    .overlay(
                        RoundedRectangle(cornerRadius: 22)
                            .stroke(card.id == selection ? ShellPalette.ember.opacity(0.45) : ShellPalette.panelBorder, lineWidth: 1)
                    )
            )
            .listRowInsets(EdgeInsets())
            .listRowBackground(Color.clear)
            .tag(card.id)
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
    }
}

private struct SessionsContentColumn: View {
    let snapshot: MonitoringSnapshot
    let onSelect: (String?) -> Void

    var body: some View {
        List(selection: selectionBinding) {
            Section {
                MonitoringSummaryRow(snapshot: snapshot)
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
            }

            if snapshot.workloads.isEmpty {
                Section {
                    MonitoringEmptyRow(
                        title: emptyTitle,
                        detail: snapshot.note
                    )
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
                }
            } else {
                Section {
                    ForEach(snapshot.workloads) { workload in
                        MonitoringWorkloadRow(workload: workload)
                            .listRowInsets(EdgeInsets())
                            .listRowBackground(Color.clear)
                            .tag(workload.id)
                    }
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
    }

    private var selectionBinding: Binding<String?> {
        Binding(
            get: { snapshot.selectedWorkloadID },
            set: { onSelect($0) }
        )
    }

    private var emptyTitle: String {
        switch snapshot.phase {
        case .connecting:
            return "Connecting to daemon"
        case .reconnecting:
            return "Waiting to reconnect"
        case .disconnected:
            return "Daemon unavailable"
        case .failed:
            return "Monitoring probe failed"
        case .connected:
            return "No live workloads"
        }
    }
}

private struct QueueContentColumn: View {
    let snapshot: MonitoringSnapshot

    var body: some View {
        List {
            Section {
                MonitoringSummaryRow(snapshot: snapshot)
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
            }

            if snapshot.queue.isEmpty {
                Section {
                    MonitoringEmptyRow(
                        title: queueTitle,
                        detail: snapshot.phase == .connected
                            ? "The daemon is reachable, but there is no queued work to display right now."
                            : snapshot.note
                    )
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
                }
            } else {
                Section {
                    ForEach(snapshot.queue) { item in
                        QueueRow(item: item)
                            .listRowInsets(EdgeInsets())
                            .listRowBackground(Color.clear)
                    }
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
    }

    private var queueTitle: String {
        switch snapshot.phase {
        case .connecting:
            return "Loading queue"
        case .reconnecting:
            return "Queue paused during reconnect"
        case .disconnected:
            return "Daemon unavailable"
        case .failed:
            return "Queue probe failed"
        case .connected:
            return "Queue is empty"
        }
    }
}

private struct DetailColumn: View {
    @EnvironmentObject private var lifecycleStore: DaemonLifecycleStore
    let selectedSection: ShellSection
    let selectedCard: ShellCard
    let transport: TransportSnapshot
    let lifecycle: DaemonLifecycleSnapshot
    let monitoring: MonitoringSnapshot

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                ShellPanel {
                    VStack(alignment: .leading, spacing: 16) {
                        HStack(alignment: .top) {
                            VStack(alignment: .leading, spacing: 8) {
                                Text(selectedSection.title.uppercased())
                                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                                    .foregroundStyle(ShellPalette.ember)
                                Text(headerTitle)
                                    .font(.system(size: 38, weight: .bold, design: .serif))
                                    .foregroundStyle(ShellPalette.ink)
                                Text(headerSummary)
                                    .font(.system(size: 15, weight: .medium, design: .rounded))
                                    .foregroundStyle(ShellPalette.mutedInk)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            Spacer(minLength: 16)
                            StatusStrip(transport: transport, lifecycle: lifecycle)
                                .frame(width: 270)
                        }

                        HStack(spacing: 12) {
                            MetricTile(title: "Project", value: transport.projectName, tone: ShellPalette.ember)
                            MetricTile(title: "Daemon URL", value: transport.daemonURL, tone: ShellPalette.moss)
                            MetricTile(title: "Lifecycle", value: lifecycle.phase.rawValue, tone: ShellPalette.brass)
                        }

                        LifecycleControlPanel(
                            lifecycle: lifecycle,
                            banner: lifecycleStore.banner,
                            diagnostics: lifecycleStore.diagnostics,
                            actionInFlight: lifecycleStore.actionInFlight,
                            onStart: lifecycleStore.requestStart,
                            onStop: lifecycleStore.requestStop
                        )
                    }
                }

                HStack(alignment: .top, spacing: 18) {
                    ShellPanel {
                        DetailHighlightsPanel(
                            section: selectedSection,
                            selectedCard: selectedCard,
                            monitoring: monitoring
                        )
                    }
                }

                ShellPanel {
                    SectionPreview(
                        section: selectedSection,
                        selectedCard: selectedCard,
                        transport: transport,
                        lifecycle: lifecycle,
                        monitoring: monitoring
                    )
                }
            }
            .padding(18)
        }
        .scrollIndicators(.hidden)
    }

    private var headerTitle: String {
        switch selectedSection {
        case .sessions:
            return monitoring.selectedDetail?.agent.taskTitle.nonEmptyValue
                ?? monitoring.selectedDetail?.session.workRef.nonEmptyValue
                ?? monitoring.selectedDetail?.workloadID
                ?? "Live session monitor"
        case .queue:
            return monitoring.queue.isEmpty ? "Queued work" : "\(monitoring.queue.count) queued item\(monitoring.queue.count == 1 ? "" : "s")"
        case .overview, .diagnostics:
            return selectedCard.title
        }
    }

    private var headerSummary: String {
        switch selectedSection {
        case .sessions:
            if let detail = monitoring.selectedDetail {
                let sessionRoute = detail.session.sessionID.nonEmptyValue ?? "pending session"
                let lifecycle = detail.lifecycleLabel
                if detail.isLive {
                    return "Showing live status, tool activity, and plain-text session output for \(sessionRoute) in \(lifecycle) state."
                }
                return "Showing retained session detail for \(sessionRoute). The live workload exited, but recent output and tool activity remain available in \(lifecycle) state."
            }
            return monitoring.note
        case .queue:
            return monitoring.queue.isEmpty ? monitoring.note : "The queue lane mirrors the daemon's waiting work without leaving the monitoring shell."
        case .overview, .diagnostics:
            return selectedCard.summary
        }
    }
}

private struct DetailHighlightsPanel: View {
    let section: ShellSection
    let selectedCard: ShellCard
    let monitoring: MonitoringSnapshot

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title)
                .font(.system(size: 12, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)
            ForEach(lines, id: \.self) { line in
                HighlightRow(text: line, accent: accent)
            }
        }
    }

    private var title: String {
        switch section {
        case .sessions:
            return "Monitoring summary"
        case .queue:
            return "Queue summary"
        case .overview, .diagnostics:
            return "Structural pillars"
        }
    }

    private var accent: Color {
        switch section {
        case .sessions:
            return ShellPalette.moss
        case .queue:
            return ShellPalette.brass
        case .overview, .diagnostics:
            return ShellPalette.ember
        }
    }

    private var lines: [String] {
        switch section {
        case .sessions:
            if let detail = monitoring.selectedDetail {
                let pidLabel = detail.agent.pid > 0 ? String(detail.agent.pid) : "pending"
                let liveSummary = detail.isLive
                    ? "Selected workload is live in the monitoring list."
                    : "Selected workload has exited; the pane is holding the last readable session detail."
                return [
                    "Selected workload: \(detail.workloadID) on pid \(pidLabel).",
                    "Session route: \(detail.session.serverRef.nonEmptyValue ?? "pending server") / \(detail.session.sessionID.nonEmptyValue ?? "pending session").",
                    "Task lane: \(detail.agent.taskID.nonEmptyValue ?? detail.session.workRef.nonEmptyValue ?? "manual spawn"). Lifecycle: \(detail.lifecycleLabel).",
                    "Recent tool calls: \(detail.toolCalls.count). Plain-text output lines cached: \(detail.eventLines.count).",
                    liveSummary,
                    detail.errors.isEmpty ? "No daemon-reported errors on the selected workload." : "Daemon reported \(detail.errors.count) error\(detail.errors.count == 1 ? "" : "s") for this workload."
                ]
            }
            return [
                "Monitoring phase: \(monitoring.phase.rawValue).",
                "Visible workloads: \(monitoring.workloads.count).",
                "Selection follows the same session when the daemon rekeys a workload after reconnect."
            ]
        case .queue:
            if monitoring.queue.isEmpty {
                return [
                    "Monitoring phase: \(monitoring.phase.rawValue).",
                    "No queued work is currently visible from the daemon.",
                    "This lane will fill with task IDs and priority order as work waits for capacity."
                ]
            }
            let highestPriority = monitoring.queue.map(\.priority).min() ?? 0
            return [
                "Queued items visible: \(monitoring.queue.count).",
                "Highest priority waiting item: P\(highestPriority).",
                "Queue rows now reflect the same HTTP status payload as the session lane."
            ]
        case .overview, .diagnostics:
            return selectedCard.highlights
        }
    }
}

private func monitoringLifecycleTone(for detail: MonitoringSelectionDetail) -> Color {
    switch detail.lifecycleLabel.lowercased() {
    case "completed", "complete", "idle":
        return ShellPalette.moss
    case "failed", "error", "errored", "crashed":
        return ShellPalette.ember
    case "exited":
        return ShellPalette.brass
    default:
        return detail.isLive ? ShellPalette.moss : ShellPalette.brass
    }
}

private func monitoringTimestampLabel(_ date: Date?) -> String {
    guard let date else {
        return "Pending"
    }
    return date.formatted(date: .abbreviated, time: .shortened)
}

private struct SectionPreview: View {
    let section: ShellSection
    let selectedCard: ShellCard
    let transport: TransportSnapshot
    let lifecycle: DaemonLifecycleSnapshot
    let monitoring: MonitoringSnapshot

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(previewLabel)
                .font(.system(size: 12, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)

            switch section {
            case .overview:
                VStack(alignment: .leading, spacing: 12) {
                    Text("The main shell is already aligned around a single operator window and a separate menu bar surface.")
                        .font(.system(size: 15, weight: .medium, design: .rounded))
                        .foregroundStyle(ShellPalette.ink)
                    HStack(spacing: 12) {
                        PreviewSlab(title: "Window", detail: "Singleton control-center scene with split navigation and detail.")
                        PreviewSlab(title: "Menu bar", detail: "Compact launcher that reopens or focuses the main window.")
                        PreviewSlab(title: "Stores", detail: "Transport, lifecycle, and navigation stay separate.")
                    }
                }
            case .sessions:
                SessionsDetailPanel(snapshot: monitoring)
            case .queue:
                QueueDetailPanel(snapshot: monitoring)
            case .diagnostics:
                VStack(alignment: .leading, spacing: 10) {
                    DiagnosticRow(label: "project", value: transport.projectName)
                    DiagnosticRow(label: "cwd", value: transport.workingDirectory)
                    DiagnosticRow(label: "daemon_url", value: transport.daemonURL)
                    DiagnosticRow(label: "daemon_reason", value: transport.daemonTargetReason)
                    DiagnosticRow(label: "cli", value: transport.cliPath)
                    DiagnosticRow(label: "lifecycle", value: lifecycle.phase.rawValue.lowercased())
                    DiagnosticRow(label: "note", value: transport.note)
                }
            }
        }
    }

    private var previewLabel: String {
        switch section {
        case .sessions:
            return "Live session detail"
        case .queue:
            return "Queue detail"
        case .overview, .diagnostics:
            return selectedCard.statusLabel == "Ready" ? "Preview canvas" : "Detail canvas"
        }
    }
}

struct MenuBarControlCenterView: View {
    @Environment(\.openWindow) private var openWindow
    @EnvironmentObject private var transportStore: TransportStore
    @EnvironmentObject private var lifecycleStore: DaemonLifecycleStore
    @EnvironmentObject private var navigationStore: NavigationStore

    let windowID: String

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Aetherflow")
                .font(.system(size: 12, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)
            Text("Control center")
                .font(.system(size: 24, weight: .bold, design: .serif))
                .foregroundStyle(ShellPalette.ink)

            HStack(spacing: 10) {
                MiniChip(text: transportStore.snapshot.phase.rawValue, tone: ShellPalette.ember)
                MiniChip(text: lifecycleStore.snapshot.phase.rawValue, tone: ShellPalette.moss)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Project: \(transportStore.snapshot.projectName)")
                Text("Daemon URL: \(transportStore.snapshot.daemonURL)")
                Text("Lane: \(navigationStore.selectedSection.title)")
            }
            .font(.system(size: 12, weight: .medium, design: .rounded))
            .foregroundStyle(ShellPalette.mutedInk)

            Divider()

            Button {
                NSApplication.shared.activate(ignoringOtherApps: true)
                openWindow(id: windowID)
            } label: {
                Text("Open Or Focus Control Center")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)

            Button {
                navigationStore.select(section: .sessions)
                NSApplication.shared.activate(ignoringOtherApps: true)
                openWindow(id: windowID)
            } label: {
                Text("Focus Sessions")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)

            HStack(spacing: 10) {
                Button("Start") {
                    lifecycleStore.requestStart()
                }
                .buttonStyle(.borderedProminent)
                .disabled(lifecycleStore.actionInFlight != nil || lifecycleStore.snapshot.phase == .running || lifecycleStore.snapshot.phase == .starting)

                Button("Stop") {
                    lifecycleStore.requestStop()
                }
                .buttonStyle(.bordered)
                .disabled(lifecycleStore.actionInFlight != nil || lifecycleStore.snapshot.phase == .stopped)
            }

            if let banner = lifecycleStore.banner {
                BannerView(banner: banner)
            }
        }
        .padding(18)
        .frame(width: 340)
        .background(AtmosphereBackground().opacity(0.92))
        .alert("Stop daemon?", isPresented: stopConfirmationPresented) {
            Button("Stop Now", role: .destructive) {
                lifecycleStore.confirmStop()
            }
            Button("Cancel", role: .cancel) {
                lifecycleStore.dismissStopConfirmation()
            }
        } message: {
            Text(lifecycleStore.pendingStopConfirmation?.message ?? "")
        }
    }

    private var stopConfirmationPresented: Binding<Bool> {
        Binding(
            get: { lifecycleStore.pendingStopConfirmation != nil },
            set: { isPresented in
                if !isPresented {
                    lifecycleStore.dismissStopConfirmation()
                }
            }
        )
    }
}

private struct LifecycleControlPanel: View {
    let lifecycle: DaemonLifecycleSnapshot
    let banner: LifecycleBanner?
    let diagnostics: [LifecycleDiagnostic]
    let actionInFlight: LifecycleAction?
    let onStart: () -> Void
    let onStop: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 12) {
                Button("Start Daemon", action: onStart)
                    .buttonStyle(.borderedProminent)
                    .disabled(actionInFlight != nil || lifecycle.phase == .running || lifecycle.phase == .starting)

                Button("Stop Daemon", action: onStop)
                    .buttonStyle(.bordered)
                    .disabled(actionInFlight != nil || lifecycle.phase == .stopped)

                if let actionInFlight {
                    MiniChip(text: actionInFlight.rawValue, tone: ShellPalette.brass)
                }
            }

            if let banner {
                BannerView(banner: banner)
            }

            HStack(spacing: 12) {
                MetricTile(title: "Sessions", value: "\(lifecycle.activeSessions)", tone: ShellPalette.ember)
                MetricTile(title: "Policy", value: lifecycle.spawnPolicy ?? "unknown", tone: ShellPalette.moss)
                MetricTile(title: "Server", value: lifecycle.serverURL ?? "unavailable", tone: ShellPalette.brass)
            }

            if !lifecycle.activeSessionIDs.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Attached sessions")
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    Text(lifecycle.activeSessionIDs.joined(separator: ", "))
                        .font(.system(size: 12, weight: .medium, design: .monospaced))
                        .foregroundStyle(ShellPalette.ink)
                        .textSelection(.enabled)
                }
            }

            VStack(alignment: .leading, spacing: 8) {
                Text("Diagnostics")
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(ShellPalette.mutedInk)
                ForEach(diagnostics) { diagnostic in
                    DiagnosticEventRow(diagnostic: diagnostic)
                }
            }
        }
    }
}

private struct BannerView: View {
    let banner: LifecycleBanner

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(banner.title)
                .font(.system(size: 13, weight: .bold, design: .rounded))
                .foregroundStyle(ShellPalette.ink)
            Text(banner.message)
                .font(.system(size: 12, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.mutedInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.34))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(bannerColor.opacity(0.45), lineWidth: 1)
                )
        )
    }

    private var bannerColor: Color {
        switch banner.tone {
        case .info:
            return ShellPalette.brass
        case .success:
            return ShellPalette.moss
        case .warning:
            return ShellPalette.ember
        case .error:
            return Color.red.opacity(0.8)
        }
    }
}

private struct DiagnosticEventRow: View {
    let diagnostic: LifecycleDiagnostic

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(diagnostic.title)
                    .font(.system(size: 12, weight: .bold, design: .rounded))
                    .foregroundStyle(ShellPalette.ink)
                Spacer()
                Text(diagnostic.timestamp.formatted(date: .omitted, time: .standard))
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(ShellPalette.mutedInk)
            }
            Text(diagnostic.detail)
                .font(.system(size: 12, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.mutedInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 16)
                .fill(Color.white.opacity(0.22))
                .overlay(
                    RoundedRectangle(cornerRadius: 16)
                        .stroke(diagnosticColor.opacity(0.35), lineWidth: 1)
                )
        )
    }

    private var diagnosticColor: Color {
        switch diagnostic.tone {
        case .info:
            return ShellPalette.brass
        case .success:
            return ShellPalette.moss
        case .warning:
            return ShellPalette.ember
        case .error:
            return Color.red.opacity(0.8)
        }
    }
}

private struct ShellPanel<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        content
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 24)
                    .fill(ShellPalette.panel)
                    .overlay(
                        RoundedRectangle(cornerRadius: 24)
                            .stroke(ShellPalette.panelBorder, lineWidth: 1)
                    )
            )
    }
}

private struct ToolbarRouteButton: View {
    let title: String
    let shortcut: Character
    let isActive: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
        }
        .keyboardShortcut(KeyEquivalent(shortcut), modifiers: [.command])
        .tint(isActive ? ShellPalette.ember : ShellPalette.moss)
    }
}

private struct MetricTile: View {
    let title: String
    let value: String
    let tone: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title.uppercased())
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)
            Text(value)
                .font(.system(size: 14, weight: .semibold, design: .rounded))
                .foregroundStyle(ShellPalette.ink)
                .lineLimit(2)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.30))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(tone.opacity(0.35), lineWidth: 1)
                )
        )
    }
}

private struct HighlightRow: View {
    let text: String
    let accent: Color

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Circle()
                .fill(accent)
                .frame(width: 8, height: 8)
                .padding(.top, 5)
            Text(text)
                .font(.system(size: 13, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

private struct MonitoringSummaryRow: View {
    let snapshot: MonitoringSnapshot

    var body: some View {
        HStack(spacing: 12) {
            MiniChip(text: snapshot.phase.rawValue.capitalized, tone: phaseTone)
            if let poolMode = snapshot.poolMode.nonEmptyValue {
                MiniChip(text: "Pool \(poolMode)", tone: ShellPalette.brass)
            }
            if let spawnPolicy = snapshot.spawnPolicy.nonEmptyValue {
                MiniChip(text: "Policy \(spawnPolicy)", tone: ShellPalette.moss)
            }
            MiniChip(text: "Workloads \(snapshot.workloads.count)", tone: ShellPalette.ember)
            MiniChip(text: "Queue \(snapshot.queue.count)", tone: ShellPalette.moss)
            Spacer(minLength: 0)
            Text(snapshot.updatedAt.formatted(date: .omitted, time: .shortened))
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.26))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(phaseTone.opacity(0.30), lineWidth: 1)
                )
        )
    }

    private var phaseTone: Color {
        switch snapshot.phase {
        case .connecting, .reconnecting:
            return ShellPalette.brass
        case .connected:
            return ShellPalette.moss
        case .disconnected, .failed:
            return ShellPalette.ember
        }
    }
}

private struct MonitoringWorkloadRow: View {
    let workload: MonitoringWorkloadSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 5) {
                    HStack(spacing: 8) {
                        Text(workload.kind == .poolAgent ? "POOL" : "SPAWN")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(workload.kind == .poolAgent ? ShellPalette.ember : ShellPalette.moss)
                        Text(workload.workRef)
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(ShellPalette.mutedInk)
                        if workload.attentionNeeded {
                            Text("ATTN")
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                .foregroundStyle(.white)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 3)
                                .background(Capsule().fill(ShellPalette.ember))
                        }
                    }
                    Text(workload.title)
                        .font(.system(size: 18, weight: .bold, design: .serif))
                        .foregroundStyle(ShellPalette.ink)
                    Text(workload.subtitle)
                        .font(.system(size: 12, weight: .medium, design: .rounded))
                        .foregroundStyle(ShellPalette.mutedInk)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                MiniChip(text: workload.lifecycleState.uppercased(), tone: workload.attentionNeeded ? ShellPalette.ember : ShellPalette.brass)
            }

            HStack(spacing: 10) {
                DetailPill(label: "role", value: workload.role)
                DetailPill(label: "session", value: workload.sessionID.nonEmptyValue ?? "claiming")
                DetailPill(label: "pid", value: workload.pid > 0 ? String(workload.pid) : "pending")
                DetailPill(label: "activity", value: workload.lastActivityAt.map(Self.relativeTimestamp) ?? "waiting")
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 20)
                .fill(Color.white.opacity(0.22))
                .overlay(
                    RoundedRectangle(cornerRadius: 20)
                        .stroke(workload.attentionNeeded ? ShellPalette.ember.opacity(0.40) : ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }

    private static func relativeTimestamp(_ date: Date) -> String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: date, relativeTo: .now)
    }
}

private struct QueueRow: View {
    let item: MonitoringQueueItem

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 6) {
                Text(item.id)
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(ShellPalette.ember)
                Text(item.title)
                    .font(.system(size: 17, weight: .semibold, design: .rounded))
                    .foregroundStyle(ShellPalette.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 12)
            MiniChip(text: "P\(item.priority)", tone: ShellPalette.brass)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 20)
                .fill(Color.white.opacity(0.22))
                .overlay(
                    RoundedRectangle(cornerRadius: 20)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct MonitoringEmptyRow: View {
    let title: String
    let detail: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(.system(size: 18, weight: .bold, design: .serif))
                .foregroundStyle(ShellPalette.ink)
            Text(detail)
                .font(.system(size: 13, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.mutedInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 20)
                .fill(Color.white.opacity(0.18))
                .overlay(
                    RoundedRectangle(cornerRadius: 20)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct SessionsDetailPanel: View {
    let snapshot: MonitoringSnapshot

    var body: some View {
        if let detail = snapshot.selectedDetail {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 16) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(detail.agent.taskTitle.nonEmptyValue ?? detail.session.workRef.nonEmptyValue ?? detail.workloadID)
                            .font(.system(size: 24, weight: .bold, design: .serif))
                            .foregroundStyle(ShellPalette.ink)
                        Text(detail.session.sessionID.nonEmptyValue ?? "Waiting for session route")
                            .font(.system(size: 12, weight: .bold, design: .monospaced))
                            .foregroundStyle(ShellPalette.mutedInk)
                        Text(detail.isLive
                             ? "The detail pane is following the live workload directly from the daemon read model."
                             : "The workload dropped out of the live list, but the selected session stays open with its last readable output and tool activity.")
                            .font(.system(size: 13, weight: .medium, design: .rounded))
                            .foregroundStyle(ShellPalette.mutedInk)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 0)
                    VStack(alignment: .trailing, spacing: 8) {
                        MiniChip(text: detail.lifecycleLabel, tone: monitoringLifecycleTone(for: detail))
                        if !detail.isLive {
                            MiniChip(text: "Retained detail", tone: ShellPalette.brass)
                        }
                    }
                }

                LazyVGrid(
                    columns: [
                        GridItem(.flexible(), spacing: 12, alignment: .leading),
                        GridItem(.flexible(), spacing: 12, alignment: .leading)
                    ],
                    spacing: 12
                ) {
                    SessionFactCard(label: "Workload", value: detail.workloadID)
                    SessionFactCard(label: "Work ref", value: detail.agent.taskID.nonEmptyValue ?? detail.session.workRef.nonEmptyValue ?? "Manual spawn")
                    SessionFactCard(label: "Session", value: detail.session.sessionID.nonEmptyValue ?? "Pending")
                    SessionFactCard(label: "Origin", value: detail.session.originType.nonEmptyValue ?? "Unknown")
                    SessionFactCard(label: "Server", value: detail.session.serverRef.nonEmptyValue ?? "Pending")
                    SessionFactCard(label: "Directory", value: detail.session.directory.nonEmptyValue ?? "Pending")
                    SessionFactCard(label: "PID", value: detail.agent.pid > 0 ? String(detail.agent.pid) : "Pending")
                    SessionFactCard(label: "Attach", value: detail.session.attachable ? "Ready" : "Pending")
                    SessionFactCard(label: "Last activity", value: monitoringTimestampLabel(detail.agent.lastActivityAt ?? detail.session.lastSeenAt))
                    SessionFactCard(label: "Updated", value: monitoringTimestampLabel(detail.session.updatedAt))
                }

                if let lastLog = detail.agent.lastLog.nonEmptyValue {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Latest daemon note")
                            .font(.system(size: 12, weight: .bold, design: .monospaced))
                            .foregroundStyle(ShellPalette.mutedInk)
                        EventLineRow(line: lastLog, tone: ShellPalette.brass)
                    }
                }

                VStack(alignment: .leading, spacing: 10) {
                    Text("Tool activity")
                        .font(.system(size: 12, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    if detail.toolCalls.isEmpty {
                        Text("No tool calls are visible yet for this session.")
                            .font(.system(size: 13, weight: .medium, design: .rounded))
                            .foregroundStyle(ShellPalette.mutedInk)
                    } else {
                        ForEach(Array(detail.toolCalls.prefix(6).enumerated()), id: \.offset) { _, call in
                            ToolCallRow(call: call)
                        }
                    }
                }

                VStack(alignment: .leading, spacing: 10) {
                    Text("Recent session output")
                        .font(.system(size: 12, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    if detail.eventLines.isEmpty {
                        Text("No plain-text session output is cached yet for this selection.")
                            .font(.system(size: 13, weight: .medium, design: .rounded))
                            .foregroundStyle(ShellPalette.mutedInk)
                    } else {
                        ForEach(Array(detail.eventLines.suffix(12).enumerated()), id: \.offset) { _, line in
                            EventLineRow(line: line, tone: detail.isLive ? ShellPalette.moss : ShellPalette.brass)
                        }
                    }
                }

                if !detail.errors.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Errors")
                            .font(.system(size: 12, weight: .bold, design: .monospaced))
                            .foregroundStyle(ShellPalette.ember)
                        ForEach(detail.errors, id: \.self) { error in
                            EventLineRow(line: error, tone: ShellPalette.ember)
                        }
                    }
                }
            }
        } else {
            MonitoringEmptyRow(
                title: snapshot.workloads.isEmpty ? "No session selected" : "Select a session lane",
                detail: snapshot.workloads.isEmpty
                    ? snapshot.note
                    : "The detail pane will hold session route, tool activity, and recent daemon events without changing selection."
            )
        }
    }
}

private struct SessionFactCard: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label.uppercased())
                .font(.system(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)
            Text(value)
                .font(.system(size: 13, weight: .semibold, design: .monospaced))
                .foregroundStyle(ShellPalette.ink)
                .textSelection(.enabled)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.22))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct QueueDetailPanel: View {
    let snapshot: MonitoringSnapshot

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if snapshot.queue.isEmpty {
                MonitoringEmptyRow(
                    title: "No queued work",
                    detail: snapshot.note
                )
            } else {
                Text("Queue priorities")
                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                    .foregroundStyle(ShellPalette.mutedInk)
                ForEach(snapshot.queue) { item in
                    HStack(alignment: .top, spacing: 12) {
                        MiniChip(text: "P\(item.priority)", tone: ShellPalette.brass)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(item.title)
                                .font(.system(size: 15, weight: .semibold, design: .rounded))
                                .foregroundStyle(ShellPalette.ink)
                            Text(item.id)
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                                .foregroundStyle(ShellPalette.mutedInk)
                        }
                        Spacer(minLength: 0)
                    }
                    .padding(12)
                    .background(
                        RoundedRectangle(cornerRadius: 18)
                            .fill(Color.white.opacity(0.22))
                            .overlay(
                                RoundedRectangle(cornerRadius: 18)
                                    .stroke(ShellPalette.panelBorder, lineWidth: 1)
                            )
                    )
                }
            }
        }
    }
}

private struct ToolCallRow: View {
    let call: DaemonToolCallPayload

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(call.tool)
                    .font(.system(size: 13, weight: .bold, design: .rounded))
                    .foregroundStyle(ShellPalette.ink)
                Spacer()
                Text(call.timestamp.formatted(date: .omitted, time: .shortened))
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(ShellPalette.mutedInk)
            }
            Text(call.title.nonEmptyValue ?? call.input.nonEmptyValue ?? "Tool invocation")
                .font(.system(size: 12, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.mutedInk)
            HStack(spacing: 10) {
                DetailPill(label: "status", value: call.status.nonEmptyValue ?? "unknown")
                DetailPill(label: "duration", value: "\(call.durationMs) ms")
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.22))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct EventLineRow: View {
    let line: String
    var tone: Color = ShellPalette.moss

    var body: some View {
        Text(line)
            .font(.system(size: 12, weight: .medium, design: .monospaced))
            .foregroundStyle(ShellPalette.ink)
            .textSelection(.enabled)
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 18)
                    .fill(Color.white.opacity(0.22))
                    .overlay(
                        RoundedRectangle(cornerRadius: 18)
                            .stroke(tone.opacity(0.35), lineWidth: 1)
                    )
            )
    }
}

private struct DetailPill: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label.uppercased())
                .font(.system(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.mutedInk)
            Text(value)
                .font(.system(size: 12, weight: .semibold, design: .monospaced))
                .foregroundStyle(ShellPalette.ink)
                .lineLimit(2)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 14)
                .fill(Color.white.opacity(0.18))
                .overlay(
                    RoundedRectangle(cornerRadius: 14)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct PreviewSlab: View {
    let title: String
    let detail: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(.system(size: 14, weight: .semibold, design: .serif))
                .foregroundStyle(ShellPalette.ink)
            Text(detail)
                .font(.system(size: 12, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.mutedInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.26))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct PreviewListRow: View {
    let title: String
    let detail: String

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            RoundedRectangle(cornerRadius: 6)
                .fill(ShellPalette.ember.opacity(0.18))
                .frame(width: 32, height: 32)
                .overlay(
                    Image(systemName: "circle.hexagongrid.fill")
                        .font(.system(size: 12))
                        .foregroundStyle(ShellPalette.ember)
                )
            VStack(alignment: .leading, spacing: 5) {
                Text(title)
                    .font(.system(size: 14, weight: .semibold, design: .rounded))
                    .foregroundStyle(ShellPalette.ink)
                Text(detail)
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .foregroundStyle(ShellPalette.mutedInk)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 18)
                .fill(Color.white.opacity(0.22))
                .overlay(
                    RoundedRectangle(cornerRadius: 18)
                        .stroke(ShellPalette.panelBorder, lineWidth: 1)
                )
        )
    }
}

private struct DiagnosticRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Text(label)
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(ShellPalette.ember)
                .frame(width: 76, alignment: .leading)
            Text(value)
                .font(.system(size: 13, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 4)
    }
}

private struct StatusStrip: View {
    let transport: TransportSnapshot
    let lifecycle: DaemonLifecycleSnapshot

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            MiniChip(text: transport.phase.rawValue, tone: ShellPalette.ember)
            MiniChip(text: lifecycle.phase.rawValue, tone: ShellPalette.moss)
            Text(transport.note)
                .font(.system(size: 12, weight: .medium, design: .rounded))
                .foregroundStyle(ShellPalette.mutedInk)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

private struct MiniChip: View {
    let text: String
    let tone: Color

    var body: some View {
        Text(text)
            .font(.system(size: 11, weight: .bold, design: .monospaced))
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(
                Capsule()
                    .fill(tone.opacity(0.16))
                    .overlay(
                        Capsule()
                            .stroke(tone.opacity(0.35), lineWidth: 1)
                    )
            )
            .foregroundStyle(ShellPalette.ink)
    }
}

private struct AtmosphereBackground: View {
    var body: some View {
        ZStack {
            LinearGradient(
                colors: [ShellPalette.canvasTop, ShellPalette.canvasBottom],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            RadialGradient(
                colors: [ShellPalette.ember.opacity(0.18), .clear],
                center: .topTrailing,
                startRadius: 40,
                endRadius: 420
            )
            RadialGradient(
                colors: [ShellPalette.moss.opacity(0.15), .clear],
                center: .bottomLeading,
                startRadius: 60,
                endRadius: 360
            )
            GridPattern()
                .opacity(0.7)
        }
        .ignoresSafeArea()
    }
}

private struct GridPattern: View {
    var body: some View {
        GeometryReader { proxy in
            Path { path in
                let step: CGFloat = 36
                let width = proxy.size.width
                let height = proxy.size.height

                stride(from: 0 as CGFloat, through: width, by: step).forEach { x in
                    path.move(to: CGPoint(x: x, y: 0))
                    path.addLine(to: CGPoint(x: x, y: height))
                }

                stride(from: 0 as CGFloat, through: height, by: step).forEach { y in
                    path.move(to: CGPoint(x: 0, y: y))
                    path.addLine(to: CGPoint(x: width, y: y))
                }
            }
            .stroke(ShellPalette.grid, lineWidth: 0.6)
        }
    }
}

private extension String {
    var nonEmptyValue: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
