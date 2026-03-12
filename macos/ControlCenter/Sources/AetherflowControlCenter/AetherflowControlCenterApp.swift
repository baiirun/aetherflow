import SwiftUI

enum SceneID {
    static let controlCenter = "control-center"
}

@main
struct AetherflowControlCenterApp: App {
    @StateObject private var transportStore: TransportStore
    @StateObject private var lifecycleStore: DaemonLifecycleStore
    @StateObject private var navigationStore: NavigationStore

    init() {
        let bootstrap = ShellBootstrapContext.detect()
        _transportStore = StateObject(wrappedValue: TransportStore(context: bootstrap))
        _lifecycleStore = StateObject(wrappedValue: DaemonLifecycleStore())
        _navigationStore = StateObject(wrappedValue: NavigationStore())
    }

    var body: some Scene {
        Window("Aetherflow Control Center", id: SceneID.controlCenter) {
            ControlCenterRootView()
                .environmentObject(transportStore)
                .environmentObject(lifecycleStore)
                .environmentObject(navigationStore)
        }
        .defaultSize(width: 1480, height: 920)

        MenuBarExtra("Aetherflow", systemImage: "hurricane") {
            MenuBarControlCenterView(windowID: SceneID.controlCenter)
                .environmentObject(transportStore)
                .environmentObject(lifecycleStore)
                .environmentObject(navigationStore)
        }
        .menuBarExtraStyle(.window)
    }
}
