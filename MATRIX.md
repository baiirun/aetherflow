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
