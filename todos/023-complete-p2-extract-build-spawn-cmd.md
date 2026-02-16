---
status: complete
priority: p2
issue_id: "023"
tags: [code-review, architecture, quality]
dependencies: []
---

# Extract shared buildSpawnCmd helper for process setup

## Problem Statement

`runForeground` and `runDetached` duplicate process setup: `strings.Fields(spawnCmdStr)`, empty check, `append(parts, prompt)`, `exec.Command`, env setup with `AETHERFLOW_AGENT_ID`, `SysProcAttr{Setsid: true}`. This same pattern also exists in `pool.go`'s `ExecProcessStarter`. If the convention changes (new env vars, different SysProcAttr), all locations need updating.

## Findings

- Found by: architecture-strategist, code-simplicity-reviewer, pattern-recognition-specialist
- Location: `cmd/af/cmd/spawn.go:140-149` (foreground), `spawn.go:188-203` (detached), `internal/daemon/pool.go` (ExecProcessStarter)
- ~10 duplicated lines between foreground and detached paths

## Proposed Solutions

### Option 1: Extract buildAgentCmd helper (Recommended)

**Approach:**
```go
func buildAgentCmd(ctx context.Context, spawnCmdStr, prompt, agentID string) *exec.Cmd {
    parts := strings.Fields(spawnCmdStr)
    if len(parts) == 0 {
        Fatal("empty spawn command")
    }
    parts = append(parts, prompt)
    cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
    cmd.Env = append(os.Environ(), "AETHERFLOW_AGENT_ID="+agentID)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
    return cmd
}
```

Callers set Stdout/Stdin/Stderr as needed.

- **Pros:** Single place for process setup, prevents divergence
- **Cons:** Minor refactor
- **Effort:** Small
- **Risk:** Low

## Technical Details

- **Affected files:** `cmd/af/cmd/spawn.go`

## Acceptance Criteria

- [ ] Process setup logic in one place
- [ ] Both foreground and detached paths use the shared helper
- [ ] No behavior change

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Found by 3 reviewers |
