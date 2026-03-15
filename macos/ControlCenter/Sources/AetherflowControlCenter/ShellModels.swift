import Foundation

enum LifecycleBannerTone {
    case info
    case success
    case warning
    case error
}

enum LifecycleAction: String {
    case starting = "Start requested"
    case stopping = "Stop requested"
}

struct LifecycleBanner: Identifiable, Equatable {
    let id = UUID()
    let tone: LifecycleBannerTone
    let title: String
    let message: String
}

struct LifecycleDiagnostic: Identifiable, Equatable {
    let id = UUID()
    let timestamp: Date
    let title: String
    let detail: String
    let tone: LifecycleBannerTone
}

struct StopConfirmationRequest: Identifiable, Equatable {
    let id = UUID()
    let activeSessions: Int
    let message: String
}

struct ShellCard: Identifiable, Hashable {
    let id: String
    let eyebrow: String
    let title: String
    let summary: String
    let statusLabel: String
    let highlights: [String]
}

enum ShellSection: String, CaseIterable, Hashable, Identifiable {
    case overview
    case sessions
    case queue
    case diagnostics

    var id: Self { self }

    var title: String {
        switch self {
        case .overview:
            return "Bridge"
        case .sessions:
            return "Sessions"
        case .queue:
            return "Queue"
        case .diagnostics:
            return "Diagnostics"
        }
    }

    var subtitle: String {
        switch self {
        case .overview:
            return "App shell and window structure."
        case .sessions:
            return "Session dashboard and detail frame."
        case .queue:
            return "Queued work and manual spawn lane."
        case .diagnostics:
            return "Transport and lifecycle readouts."
        }
    }

    var symbolName: String {
        switch self {
        case .overview:
            return "square.split.2x1"
        case .sessions:
            return "rectangle.stack.person.crop"
        case .queue:
            return "list.bullet.clipboard"
        case .diagnostics:
            return "waveform.path.ecg.rectangle"
        }
    }

    var cards: [ShellCard] {
        switch self {
        case .overview:
            return [
                ShellCard(
                    id: "overview-window",
                    eyebrow: "Window",
                    title: "Single control-center scene",
                    summary: "The app opens into one durable main shell with sidebar, content list, and detail pane already in place.",
                    statusLabel: "Ready",
                    highlights: [
                        "Main window is modeled as the canonical control-center surface.",
                        "Toolbar routing stays attached to the same scene instead of spawning detached views.",
                        "The layout leaves room for lifecycle actions without structural churn."
                    ]
                ),
                ShellCard(
                    id: "overview-state",
                    eyebrow: "State",
                    title: "Stores stay split by responsibility",
                    summary: "Transport bootstrap, daemon lifecycle, and navigation remain separate so later tasks can grow them independently.",
                    statusLabel: "Split",
                    highlights: [
                        "Bootstrap owns endpoint discovery only.",
                        "Lifecycle stays daemon-shaped rather than absorbing transport errors.",
                        "Navigation owns section and row selection."
                    ]
                ),
            ]
        case .sessions:
            return [
                ShellCard(
                    id: "sessions-dashboard",
                    eyebrow: "Dashboard",
                    title: "Native list-detail session lane",
                    summary: "The center column is a native selectable list, ready to host session-first rows without replacing the shell.",
                    statusLabel: "Native",
                    highlights: [
                        "Selection is list-backed instead of button-backed.",
                        "Keyboard traversal and focus restoration can build on native behavior.",
                        "The detail lane remains stable while live data arrives."
                    ]
                ),
                ShellCard(
                    id: "sessions-detail",
                    eyebrow: "Detail",
                    title: "Dedicated detail host",
                    summary: "The detail pane is reserved for the selected session’s header, status, and activity surfaces.",
                    statusLabel: "Framed",
                    highlights: [
                        "Detail content hangs off a selected item, not off global app state.",
                        "The pane can absorb timeline and handoff surfaces later.",
                        "No layout rework is required to add agent detail or event feed data."
                    ]
                ),
            ]
        case .queue:
            return [
                ShellCard(
                    id: "queue-work",
                    eyebrow: "Queue",
                    title: "Work lane inside the main shell",
                    summary: "Queued work and manual spawns already have a reserved column without becoming a separate app mode.",
                    statusLabel: "Reserved",
                    highlights: [
                        "Queue state can slot into the existing list lane.",
                        "Selection and detail semantics match the session lane.",
                        "The shell can show priority and attention without introducing a new top-level surface."
                    ]
                ),
            ]
        case .diagnostics:
            return [
                ShellCard(
                    id: "diagnostics-transport",
                    eyebrow: "Bootstrap",
                    title: "Transport context is visible",
                    summary: "Project, working directory, and daemon URL are visible before live connectivity work lands.",
                    statusLabel: "Visible",
                    highlights: [
                        "Bootstrap assumptions are operator-readable.",
                        "The shell can distinguish endpoint setup from daemon lifecycle state.",
                        "Later polling can replace these placeholders in-place."
                    ]
                ),
                ShellCard(
                    id: "diagnostics-lifecycle",
                    eyebrow: "Lifecycle",
                    title: "Daemon state has its own lane",
                    summary: "Lifecycle readouts stay separate from reachability and selection, matching the backend contract.",
                    statusLabel: "Scoped",
                    highlights: [
                        "Starting, running, stopping, stopped, and failed states have a dedicated home.",
                        "Active session count can surface here without muddling navigation.",
                        "Failure copy can evolve without taking over the whole app."
                    ]
                ),
            ]
        }
    }
}

enum TransportPhase: String {
    case primed = "Daemon URL resolved"
    case unreachable = "Daemon unreachable"
    case connected = "Connected"
}

struct TransportSnapshot {
    let phase: TransportPhase
    let projectName: String
    let workingDirectory: String
    let daemonURL: String
    let cliPath: String
    let daemonTargetReason: String
    let note: String
}

enum DaemonLifecyclePhase: String {
    case starting = "Starting"
    case running = "Running"
    case stopping = "Stopping"
    case stopped = "Stopped"
    case failed = "Failed"
}

struct DaemonLifecycleSnapshot {
    let phase: DaemonLifecyclePhase
    let activeSessions: Int
    let activeSessionIDs: [String]
    let serverURL: String?
    let spawnPolicy: String?
    let statusCopy: String
    let lastError: String?
    let updatedAt: Date
}
