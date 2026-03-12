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
            socketPath: context.socketPath,
            note: "Shell bootstrap is resolved. Live probing and reconnect behavior land in the follow-up connectivity task."
        )
    }
}

@MainActor
final class DaemonLifecycleStore: ObservableObject {
    @Published private(set) var snapshot = DaemonLifecycleSnapshot(
        phase: .stopped,
        activeSessions: 0,
        statusCopy: "Shell is ready. Live daemon polling and lifecycle actions layer into this lane next.",
        lastError: nil,
        updatedAt: .now
    )
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
