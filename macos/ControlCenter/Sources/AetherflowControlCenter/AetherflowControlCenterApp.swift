import SwiftUI

enum SceneID {
    static let controlCenter = "control-center"
}

@main
struct AetherflowControlCenterApp: App {
    @StateObject private var transportStore: TransportStore
    @StateObject private var lifecycleStore: DaemonLifecycleStore
    @StateObject private var monitoringStore: MonitoringStore
    @StateObject private var navigationStore: NavigationStore

    init() {
        let bootstrap = ShellBootstrapContext.detect()
        let transportStore = TransportStore(context: bootstrap)
        _transportStore = StateObject(wrappedValue: transportStore)
        _lifecycleStore = StateObject(wrappedValue: DaemonLifecycleStore(context: bootstrap, transportStore: transportStore))
        _monitoringStore = StateObject(wrappedValue: MonitoringStore(context: bootstrap))
        _navigationStore = StateObject(wrappedValue: NavigationStore())
    }

    var body: some Scene {
        Window("Aetherflow Control Center", id: SceneID.controlCenter) {
            ControlCenterRootView()
                .environmentObject(transportStore)
                .environmentObject(lifecycleStore)
                .environmentObject(monitoringStore)
                .environmentObject(navigationStore)
        }
        .defaultSize(width: 1480, height: 920)

        MenuBarExtra("Aetherflow", systemImage: "hurricane") {
            MenuBarControlCenterView(windowID: SceneID.controlCenter)
                .environmentObject(transportStore)
                .environmentObject(lifecycleStore)
                .environmentObject(monitoringStore)
                .environmentObject(navigationStore)
        }
        .menuBarExtraStyle(.window)
    }
}
