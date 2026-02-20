# Feature Matrix

Expected behaviors and their verification status. This is the project's oracle — an explicit map of "what the system should do."

**Planners** add rows when creating tasks. **Workers** update coverage when implementing.

| Feature | Behavior | Verification | Coverage | Last Verified |
|---------|----------|--------------|----------|---------------|
| Daemon | Poll loop reads ready tasks from prog | `go test ./internal/daemon/... -run TestPoll` | covered | 2025-02-06 |
| Daemon | Role inference returns worker for all tasks (MVP) | `go test ./internal/daemon/... -run TestInferRole` | covered | 2025-02-06 |
| Daemon | Pool respects concurrency limit | `go test ./internal/daemon/... -run TestPool` | covered | 2025-02-06 |
| Daemon | Crashed agents are respawned up to max retries | `go test ./internal/daemon/... -run TestReap` | covered | 2025-02-06 |
| Daemon | Config loads from YAML file with CLI flag override | `go test ./internal/daemon/... -run TestConfig` | covered | 2025-02-06 |
| Daemon | Unix socket RPC responds to status and shutdown | `go test ./internal/daemon/... -run TestDaemon` | covered | 2025-02-06 |
| Pool | Tracks last 20 exited agents in ring buffer with exit state | `go test ./internal/daemon/... -run TestPoolRecentHistory` | covered | 2026-02-10 |
| Pool | Ring buffer wraps correctly when exceeding capacity | `go test ./internal/daemon/... -run TestPoolRecentHistoryRingBuffer` | covered | 2026-02-10 |
| Status | af status displays Recent section with exited agents | Manual: `af status` (daemon required) | manual | 2026-02-10 |
| Events | Session ID captured from plugin session.created event | `go test ./internal/daemon/... -run TestClaimSessionPoolAgent` | covered | 2026-02-19 |
| Status | Agent detail shows opencode session ID in TUI meta pane | Manual: TUI agent detail view | manual | 2026-02-10 |
| Agent Control | af kill <agent> sends SIGTERM and validates agent state | `go test ./internal/daemon/... -run TestHandleAgentKill` | covered | 2026-02-10 |
| Agent Control | Kill rejects invalid PIDs (0 or negative) | `go test ./internal/daemon/... -run TestHandleAgentKillInvalidPID` | covered | 2026-02-10 |
| Agent Control | Kill rejects non-running agents | `go test ./internal/daemon/... -run TestHandleAgentKillNonRunningAgent` | covered | 2026-02-10 |
