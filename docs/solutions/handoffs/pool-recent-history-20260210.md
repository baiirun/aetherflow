# Pool History Tracking for Recently Exited Agents

**Date:** 2026-02-10  
**Task:** ts-c8f6e2  
**Epic:** ep-0641d1 (TUI: Interactive daemon monitor)  
**PR:** https://github.com/baiirun/aetherflow/pull/2

## What was done

Implemented a bounded ring buffer (size 20) in the Pool to track recently exited agents. The pool now captures exit state (clean/crashed), exit code, duration, and task metadata for all agents that exit, whether they complete successfully or crash.

The `af status` command displays a new "Recent" section between the active agents and the queue, showing the last few exited agents with visual indicators for clean (✓) vs crashed (✗) exits.

## Original requirement

From the task DoD:
> The pool keeps a bounded ring buffer of recently exited agents (last N, e.g. 20) with their exit state (clean/crashed), exit code, duration, and task ID. af status shows a 'Recent' section with the last few completed/crashed agents. This enables the 'Recent' section from the observability design doc.

This is part of the TUI dashboard epic, which called for showing historical agent activity, not just current running agents.

## Files modified

**internal/daemon/pool.go**
- Added `RecentAgent` struct with exit metadata (ID, task ID, role, spawn/exit times, exit state, exit code, duration)
- Added `ExitState` type (`ExitClean`, `ExitCrashed`)
- Added ring buffer fields to `Pool` struct: `recentBuf`, `recentHead`, `recentSize`
- Modified `NewPool` to initialize ring buffer with fixed size 20
- Modified `reap()` to push exited agents to ring buffer on every exit (clean or crashed)
- Added `Recent()` method to return ring buffer contents in reverse chronological order

**internal/daemon/status.go**
- Added `Recent` field to `FullStatus` struct
- Modified `BuildFullStatus` to populate `Recent` from `pool.Recent()`

**internal/client/client.go**
- Added `RecentAgent` struct (client-side type mirror)
- Added `Recent` field to `FullStatus` struct

**cmd/af/cmd/status.go**
- Modified `printStatus()` to display "Recent" section between active agents and queue
- Added `formatDuration()` helper (similar to `formatUptime` but operates on `time.Duration` instead of `time.Time`)
- Recent section shows agent ID, task ID, duration, role, exit indicator (✓/✗), and exit code
- Limited display to first 5 recent agents to avoid clutter

## Implementation approach

**Ring buffer design:**
- Fixed-size array (`recentHistorySize = 20`)
- Head pointer tracks next write position
- Size counter tracks number of valid entries (0 to capacity)
- On push: write to `recentBuf[recentHead]`, advance head with modulo wrap, increment size up to capacity

**Reverse chronological ordering:**
- `Recent()` iterates backward from head-1, wrapping around the buffer
- Returns entries with most recent first (Recent[0] is the most recently exited agent)

**Exit state classification:**
- `ExitClean`: `proc.Wait()` returned nil (exit code 0)
- `ExitCrashed`: `proc.Wait()` returned error (non-zero exit code or process signal)

**Integration points:**
- Exit tracking happens in `reap()` under the same lock that removes the agent from the active map
- No new RPC methods — existing `status.full` RPC now includes `Recent` field
- Display is passive (no user interaction) — just renders the buffer contents

## Tests added

**TestPoolRecentHistory**
- Spawns 2 agents, releases them cleanly in order
- Verifies they appear in Recent in reverse chronological order
- Checks exit state is `ExitClean`, exit code is 0

**TestPoolRecentHistoryCrash**
- Spawns an agent that crashes (MaxRetries=3, so 4 total attempts)
- Verifies all crash attempts appear in Recent with `ExitCrashed` state and non-zero exit codes

**TestPoolRecentHistoryRingBuffer**
- Spawns `recentHistorySize + 5` agents (25 total) to overflow the buffer
- Verifies only the last 20 are retained
- Checks the most recent and oldest entries are correct (wrapping happened)

All tests use fake processes with controlled release timing to ensure deterministic ordering.

## What was tried and didn't work

N/A — straightforward implementation, no blocked approaches.

## Key decisions

**Ring buffer size (20):**
- Chosen based on the task description ("last N, e.g. 20")
- Large enough to show meaningful recent activity
- Small enough to avoid memory bloat (each RecentAgent is ~100 bytes)

**Display limit (5 in status output):**
- Full buffer contains 20, but showing all would clutter the status view
- 5 is enough to see recent pattern without overwhelming the display
- Full history available via JSON output or future TUI drill-down

**Exit state classification (clean vs crashed):**
- Simple binary: Wait() error means crashed, nil means clean
- More nuanced states (OOMKilled, timeout, signal) deferred — can add later if needed

**No filtering by task:**
- Ring buffer is global across all tasks
- Respawns create separate entries (same task ID, different agent IDs)
- This matches the observability goal: see what the pool is doing, not per-task history

## Remaining concerns

None. Implementation is complete, tests pass, PR created.

## Next steps

- Merge PR after review
- TUI dashboard (next epic task) will consume this data for the Agent Master Panel
