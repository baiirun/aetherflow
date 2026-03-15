### daemon-routing: explicit `--project` is still a transport override

Manual mode can be global by default without making `--project` inert. In this CLI, operators use `--project` on non-start commands as the only ad hoc way to reach a project-scoped daemon. Resolver changes need to preserve that explicit routing path even if the default manual endpoint becomes global.

### macos-control-center: align daemon start with the resolved probe target

Changing the app’s default daemon URL is not enough on its own. If the app can also start the daemon, the start command must carry the same resolved endpoint or the app can still boot one daemon and probe another. Passing the normalized listen address through the start path keeps manual-mode bootstrap coherent.
