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
    @EnvironmentObject private var navigationStore: NavigationStore

    var body: some View {
        NavigationSplitView {
            SidebarColumn(selection: navigationStore.selectedSectionBinding)
        } content: {
            ContentColumn(cards: navigationStore.cards, selection: navigationStore.selectedCardBinding)
        } detail: {
            DetailColumn(
                selectedSection: navigationStore.selectedSection,
                selectedCard: navigationStore.selectedCard,
                transport: transportStore.snapshot,
                lifecycle: lifecycleStore.snapshot
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
    let cards: [ShellCard]
    @Binding var selection: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ShellPanel {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Operator lanes")
                        .font(.system(size: 12, weight: .bold, design: .monospaced))
                        .foregroundStyle(ShellPalette.mutedInk)
                    Text("The center lane hosts the active story of the product surface: what this section needs to show, and how it will expand when live data arrives.")
                        .font(.system(size: 14, weight: .medium, design: .rounded))
                        .foregroundStyle(ShellPalette.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

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
        .padding(18)
    }
}

private struct DetailColumn: View {
    @EnvironmentObject private var lifecycleStore: DaemonLifecycleStore
    let selectedSection: ShellSection
    let selectedCard: ShellCard
    let transport: TransportSnapshot
    let lifecycle: DaemonLifecycleSnapshot

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
                                Text(selectedCard.title)
                                    .font(.system(size: 38, weight: .bold, design: .serif))
                                    .foregroundStyle(ShellPalette.ink)
                                Text(selectedCard.summary)
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
                        VStack(alignment: .leading, spacing: 12) {
                            Text("Structural pillars")
                                .font(.system(size: 12, weight: .bold, design: .monospaced))
                                .foregroundStyle(ShellPalette.mutedInk)
                            ForEach(selectedCard.highlights, id: \.self) { line in
                                HighlightRow(text: line, accent: ShellPalette.ember)
                            }
                        }
                    }
                }

                ShellPanel {
                    SectionPreview(section: selectedSection, transport: transport, lifecycle: lifecycle)
                }
            }
            .padding(18)
        }
        .scrollIndicators(.hidden)
    }
}

private struct SectionPreview: View {
    let section: ShellSection
    let transport: TransportSnapshot
    let lifecycle: DaemonLifecycleSnapshot

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Preview canvas")
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
                HStack(alignment: .top, spacing: 12) {
                    VStack(alignment: .leading, spacing: 10) {
                        ForEach([
                            "Session dashboard row",
                            "Selection-backed row",
                            "Detail-ready row",
                        ], id: \.self) { session in
                            PreviewListRow(title: session, detail: "Native list selection and detail updates stay aligned.")
                        }
                    }
                    VStack(alignment: .leading, spacing: 10) {
                        PreviewSlab(title: "Header host", detail: "Session identity and state.")
                        PreviewSlab(title: "Activity host", detail: "Timeline and events stack below.")
                    }
                }
            case .queue:
                VStack(alignment: .leading, spacing: 10) {
                    PreviewListRow(title: "Queued work row", detail: "Queue data can reuse the list-detail shell.")
                    PreviewListRow(title: "Manual spawn row", detail: "Spawn state can live in the same grammar.")
                }
            case .diagnostics:
                VStack(alignment: .leading, spacing: 10) {
                    DiagnosticRow(label: "project", value: transport.projectName)
                    DiagnosticRow(label: "cwd", value: transport.workingDirectory)
                    DiagnosticRow(label: "daemon_url", value: transport.daemonURL)
                    DiagnosticRow(label: "cli", value: transport.cliPath)
                    DiagnosticRow(label: "lifecycle", value: lifecycle.phase.rawValue.lowercased())
                    DiagnosticRow(label: "note", value: transport.note)
                }
            }
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
