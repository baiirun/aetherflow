### daemon-routing: explicit `--project` is still a transport override

Manual mode can be global by default without making `--project` inert. In this CLI, operators use `--project` on non-start commands as the only ad hoc way to reach a project-scoped daemon. Resolver changes need to preserve that explicit routing path even if the default manual endpoint becomes global.
