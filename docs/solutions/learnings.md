### daemon-routing: explicit `--project` is still a transport override

Manual mode can be global by default without making `--project` inert. In this CLI, operators use `--project` on non-start commands as the only ad hoc way to reach a project-scoped daemon. Resolver changes need to preserve that explicit routing path even if the default manual endpoint becomes global.

### macos-control-center: align daemon start with the resolved probe target

Changing the app’s default daemon URL is not enough on its own. If the app can also start the daemon, the start command must carry the same resolved endpoint or the app can still boot one daemon and probe another. Passing the normalized listen address through the start path keeps manual-mode bootstrap coherent.

### monitoring-ui: menu bar deep links need an immediate refresh

Selecting a workload from the menu bar is not enough if the monitoring store only polls on an interval. The selection changes immediately, but the detail pane can stay blank until the next refresh. When a menu bar action is supposed to open a specific session, trigger an explicit refresh after updating selection so the operator lands on populated detail instead of a transient empty state.
