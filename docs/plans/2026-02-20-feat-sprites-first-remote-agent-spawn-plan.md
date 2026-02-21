---
title: "feat: sprites-first remote agent spawn with session attach"
type: feat
date: 2026-02-20
updated: 2026-02-21
brainstorm: docs/brainstorms/2026-02-20-remote-sandbox-spawn-brainstorm.md
---

# Sprites-first remote agent spawn with session attach

## Overview

Enable Aetherflow to spawn agents in Sprites and attach humans to those remote sessions using the existing `af session attach` workflow.

MVP is intentionally narrow:

- Spawn remote agent runtime on Sprites.
- Capture and persist enough metadata to re-attach.
- Keep provider-native escape hatches (users can use `sprite` CLI/API directly after spawn).

Out of scope for MVP:

- Multi-provider abstractions/adapters.
- AF-managed wrappers for every provider capability (port logs, previews, desktop views).

## Problem Statement / Motivation

Today, `af` is effectively local-only for attach targets and server URL validation (`internal/daemon/server_url.go:10`). This blocks the immediate goal: run agents remotely while keeping Aetherflow as orchestration entrypoint.

Without a minimal remote spawn path, users cannot:

- Offload execution to remote sandbox infra.
- Keep local machine clean/lightweight.
- Share persistent agent environments while still using Aetherflow session UX.

## Proposed Solution

Add a Sprites-specific MVP flow with strict boundaries:

1. `af spawn --provider sprites ...` (or equivalent config-driven mode) requests a remote Sprite and starts agent execution there.
2. Aetherflow persists spawn/session routing metadata in the daemon's registry.
3. `af sessions` and `af session attach <session-id>` continue to work, now including Sprites-backed sessions.
4. All non-core follow-up interactions (advanced networking, deep logs, custom control) remain provider-native via Sprites CLI/API.

## Research Summary

### Local findings used for this plan

- Existing spawn and attach flows are already server-first and registry-backed: `cmd/af/cmd/spawn.go:61`, `cmd/af/cmd/sessions.go:339`, `internal/sessions/store.go:120`.
- Current server URL validation only permits localhost/127.0.0.1 and blocks remote targets: `internal/daemon/server_url.go:25`.
- Daemon/event model and plugin pipeline are in place and should be reused rather than replaced: `internal/daemon/daemon.go:347`, `internal/install/plugins/aetherflow-events.ts:56`.
- Existing roadmap/docs explicitly reference remote sandboxes/Sprites direction: `README.md:648`.

### Institutional learnings applied

- Path/input validation must happen at boundaries; keep defense in depth for IDs and paths: `docs/solutions/security-issues/path-traversal-validation-pattern-20260210.md`.
- Project/socket isolation and process isolation patterns are security-critical when expanding orchestration surfaces: `docs/solutions/security-issues/daemon-cross-project-shutdown-socket-isolation-20260207.md`.
- Constructor defaults for non-YAML fields prevent nil-panic regressions in daemon/RPC surfaces: `docs/solutions/runtime-errors/nil-pointer-status-handler-runner-not-set-20260207.md`.
- Reconcile orphaned state on restart to recover from partial/crash states: `docs/solutions/runtime-errors/orphaned-in-progress-tasks-reclaim-on-startup-20260207.md`.

### External findings used for this plan

- Sprites supports create/exec/proxy primitives and token auth for automation: `https://sprites.dev/api/sprites`, `https://sprites.dev/api/sprites/exec`, `https://docs.sprites.dev/cli/authentication/`.
- Sprites lifecycle and persistence model is suitable for long-lived agent sessions (warm/cold, persistent filesystem): `https://docs.sprites.dev/working-with-sprites/`.
- Security baseline for external integration should include strict HTTPS/TLS validation, allowlist-style trust policy, and idempotent request behavior: `https://cheatsheetseries.owasp.org/cheatsheets/Transport_Layer_Security_Cheat_Sheet.html`, `https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html`, `https://google.aip.dev/155`.

## Technical Considerations

- **Security**
  - Remote target policy must default to secure values (HTTPS, cert validation, explicit trust config).
  - Sprites credentials should be sourced securely (env/OS keychain path), never logged, and redacted in status/errors.
- **Correctness**
  - Spawn must be idempotent to avoid duplicate remote environments on retries.
  - Session discovery relies on the plugin event pipeline reaching the daemon over HTTP.
- **Observability**
  - Attach lifecycle should emit consistent start/end events with `request_id`, `provider`, `sandbox_id`, `session_id`, status, and duration.
- **Compatibility**
  - Existing non-remote spawn path must remain intact.
  - Existing `af session attach` UX should remain unchanged for end users.

## Daemon Transport (updated 2026-02-21)

### Design decision: HTTP replaces Unix socket

The daemon's RPC transport is migrating from Unix socket to HTTP. This is a prerequisite for remote spawn support — the `aetherflow-events` plugin running inside a remote sprite needs to POST events back to the daemon, which is impossible over a Unix socket.

Key decisions:

- **HTTP is the primary and only transport.** The Unix socket is removed, not kept alongside.
- **No auth for MVP.** Bearer token auth will be added later. For now, rely on network isolation (localhost binding).
- **Daemon is a server that can eventually run anywhere** — locally, on a VPS, in the cloud. This is the foundation for a centralized coordination service, not just a local process.
- **Streaming via polling for now.** `af logs -f` currently polls `events.list` every 500ms. This pattern works over HTTP unchanged. SSE (server-sent events) can be added later as an optimization for push-based streaming.

### What changes

| Component | Before (Unix socket) | After (HTTP) |
|---|---|---|
| Daemon listener | `net.Listen("unix", path)` | `net/http` server on `localhost:port` |
| Plugin transport | `net.createConnection(socketPath)` + raw JSON | `fetch(daemonURL + "/session/event", { method: "POST" })` |
| Plugin discovery | `AETHERFLOW_SOCKET` env var | `AETHERFLOW_URL` env var (e.g. `http://localhost:7070`) |
| CLI client | `net.Dial("unix", path)` + raw JSON | `http.Client` to daemon URL |
| CLI discovery | Socket path from project/config | Daemon URL from config or `AETHERFLOW_URL` |
| Auth | Filesystem permissions (0700) | None for MVP; bearer token later |
| Protocol | Newline-delimited JSON over stream | Standard HTTP request/response with JSON bodies |

### RPC method → HTTP endpoint mapping

| Current RPC method | HTTP endpoint | Method |
|---|---|---|
| `session.event` | `POST /api/v1/events` | POST |
| `events.list` | `GET /api/v1/events?session_id=X&after=T` | GET |
| `status.full` | `GET /api/v1/status` | GET |
| `status.agent` | `GET /api/v1/status/agents/:id` | GET |
| `pool.drain` | `POST /api/v1/pool/drain` | POST |
| `pool.pause` | `POST /api/v1/pool/pause` | POST |
| `pool.resume` | `POST /api/v1/pool/resume` | POST |
| `spawn.register` | `POST /api/v1/spawns` | POST |
| `spawn.deregister` | `DELETE /api/v1/spawns/:id` | DELETE |
| `shutdown` | `POST /api/v1/shutdown` | POST |

### Session discovery via plugin events over HTTP

Session discovery does not require a reconcile loop or polling. The existing `claimSession` mechanism works unchanged — the only difference is transport:

1. Daemon starts agent on a sprite (via Sprites API).
2. Agent starts an opencode session on the sprite.
3. The `aetherflow-events` plugin inside that opencode session fires a `session.created` event.
4. Plugin POSTs the event to the daemon at `AETHERFLOW_URL` (instead of writing to `AETHERFLOW_SOCKET`).
5. Daemon's `claimSession` correlates the `session_id` to the unclaimed spawn, same as local.
6. Spawn record transitions from `spawning` → `running` with `session_id` bound.

This means the plugin needs two changes: support `AETHERFLOW_URL` as an alternative to `AETHERFLOW_SOCKET`, and use HTTP POST instead of raw socket write when `AETHERFLOW_URL` is set.

## Operational Contracts (MVP)

### Canonical identity and handle contract

- Aetherflow creates and returns a canonical `spawn_id` for every remote spawn request.
- `spawn_id` is the only guaranteed handle before `session_id` is discovered.
- `session_id` becomes an alias once known; both must resolve to the same canonical spawn record.
- `af sessions` must show both fields when available.
- `af session attach <id>` resolution order: exact `session_id` match, then exact `spawn_id` match, else error.

Persistence requirement:

- `spawn_id` is persisted as first-class field and indexed for lookup.
- `session_id` is optional index that may be null until discovery completes.

Golden-path CLI behavior:

```text
af spawn --provider sprites "prompt"
-> state=spawning spawn_id=spn_123 session_id=<pending>

af session attach spn_123
-> pending: session not attachable yet (exit 3)

[plugin event arrives via HTTP: session_id=ses_abc bound to spn_123]

af session attach spn_123
-> attaches normally (exit 0)

af session attach ses_abc
-> attaches normally (exit 0)
```

### Spawn/attach state machine

| State | Meaning | Allowed transitions | Attach behavior |
|---|---|---|---|
| `requested` | Request accepted locally; remote create not started yet | `spawning`, `failed` | Returns pending message with retry hint |
| `spawning` | Remote provider call in progress | `running`, `failed`, `unknown` | Returns pending message with current provider operation id if available |
| `running` | Remote runtime exists and session is attachable | `terminated`, `unknown` | Normal attach path |
| `failed` | Spawn failed with terminal non-retryable error | none | Attach rejected with actionable reason |
| `terminated` | Runtime/session intentionally ended | none | Attach rejected (terminated) |
| `unknown` | Non-terminal uncertainty (partial failure, timeout, crash mid-transition) | `spawning`, `running`, `failed`, `terminated` | Attach returns pending/unknown and triggers reconcile |

Attach attempts before readiness (`requested`, `spawning`, `unknown`) must not fail silently; CLI returns deterministic non-zero exit and a machine-readable pending/unknown reason.

Unknown-state resolution:

- If `unknown` remains unresolved after 120 seconds (`T_RECONCILE_TIMEOUT`), mark record `failed` with code `RECONCILE_TIMEOUT`.
- If provider reports runtime alive at timeout, keep local record in `failed` with `provider_runtime_alive=true` and print explicit operator command to rebind or terminate.
- After `failed`, automatic retries stop; only explicit user action (new `af spawn` request) or provider-native action can transition again.

JSON contract for non-ready attach:

```json
{
  "success": false,
  "code": "SESSION_NOT_READY",
  "state": "spawning",
  "spawn_id": "spn_123",
  "session_id": null,
  "retry_after_seconds": 5
}
```

### Idempotency policy

- Client generates `request_id` (UUIDv4) for each logical spawn attempt.
- Idempotency scope is `(provider, project, request_id)`.
- Idempotency record TTL is 24 hours for MVP.
- Normalized payload for conflict checks is exactly: `{provider, project, prompt, model, cwd, env_allowlist, timeout}` with lexicographically sorted keys and sorted `env_allowlist` values.
- Same `(provider, project, request_id)` + same normalized payload returns the original result (no duplicate sandbox creation).
- Same idempotency key with different payload returns conflict error (do not execute new remote spawn).

### Concurrency guardrails

- Enforce unique constraint on `(provider, project, request_id)` in local spawn metadata store.
- Acquire per-key lock before provider create call; release only after record reaches terminal or persisted in-progress state.
- If duplicate in-flight request arrives, return existing record immediately (never call provider create twice).

### Correlation metadata contract

- Every provider create request must include both `spawn_id` and `request_id` as runtime tags/metadata.
- Event/session correlation must require one of: exact `spawn_id` tag match, exact `request_id` match.
- If neither correlation key is present, session remains unbound; never bind by heuristic timestamp/title matching.

### Retry/backoff policy

- **Retryable:** network timeouts, connection resets, HTTP 429, HTTP 5xx.
- **Non-retryable:** HTTP 400/401/403/404 and validation errors.
- Backoff: exponential with jitter, capped attempts and capped total wall-clock budget per spawn.
- Honor `Retry-After` when present.
- Exhausted retries move state to `unknown` (not `failed`) unless a non-retryable error is observed.

### Attach auth and transport contract

- `af session attach` always targets a previously persisted `server_ref` from spawn metadata; it never accepts arbitrary runtime URLs.
- Attach credentials are sourced from the same credential resolver used for spawn (env first, then keychain-backed store when configured).
- Missing/expired credentials return deterministic errors (`ATTACH_AUTH_MISSING`, `ATTACH_AUTH_EXPIRED`) with non-zero exit.
- Attach transport enforces the same HTTPS/TLS trust policy as spawn target validation.

### Session discovery contract

- Session discovery relies on the plugin event pipeline over HTTP.
- When the opencode session starts on the sprite, the plugin POSTs `session.created` to the daemon.
- The daemon's `claimSession` binds the `session_id` to the unclaimed spawn record.
- No polling or reconcile loop is needed for session discovery — it is event-driven.
- If `session_id` is not discovered (plugin unreachable, event lost), the spawn stays in `spawning` and the user sees `SESSION_NOT_READY` on attach.

### Error code taxonomy (MVP)

- `SESSION_NOT_READY`
- `IDEMPOTENCY_CONFLICT`
- `UNTRUSTED_ENDPOINT`
- `ATTACH_AUTH_MISSING`
- `ATTACH_AUTH_EXPIRED`
- `PROVIDER_RATE_LIMITED`
- `PROVIDER_UNAVAILABLE`

### Trust policy defaults

- HTTPS only for remote endpoints by default.
- Full TLS certificate and hostname verification required.
- Reject loopback/private-link-local targets unless explicitly allowlisted in config.
- No insecure TLS skip mode in MVP.
- Any proxy/CA customization must be explicit and logged at startup (without secret values).

Trusted endpoint derivation:

- `server_ref` must resolve from trusted local config/provider resolver, not arbitrary provider-returned URL strings.
- Provider-returned URLs are treated as data and must pass the same trust-policy validation before use.

Examples of default rejections:

- `http://...` endpoint.
- Endpoint with certificate hostname mismatch.
- Endpoint resolving to blocked private/link-local ranges without explicit allowlist.

## Architecture Sketch

```mermaid
flowchart LR
  U[af CLI] -->|HTTP| D[daemon HTTP API]
  D --> P[Sprites API]
  P --> S[Sprite Runtime]
  S --> O[OpenCode Session]
  O --> E[aetherflow-events plugin]
  E -->|HTTP POST| D
  D --> R[session registry + spawn metadata store]
  U --> A[af session attach]
  A --> O
```

## Data Model Additions (MVP)

Persist minimal remote spawn metadata (new or extended store), keyed by spawn/request id:

- `spawn_id` (canonical local handle)
- `provider` (fixed: `sprites` in MVP)
- `provider_sandbox_id`
- `provider_operation_id` (if available)
- `server_ref` / attach endpoint
- `session_id` (once known)
- `request_id` (idempotency key)
- `state` (`requested|spawning|running|failed|terminated|unknown`)
- `created_at`, `updated_at`
- `last_error` (redacted)

Store schema/versioning:

- Add explicit `schema_version` for remote spawn metadata store.
- Include forward-compatible reader behavior (ignore unknown fields).
- Include rollback behavior: if newer schema is detected, daemon starts read-only for this store and emits actionable upgrade warning.

## Ownership and boundaries (MVP)

- `remote_spawn_store` is the single source of truth for remote spawn lifecycle fields: `spawn_id`, `request_id`, `provider_*`, lifecycle `state`.
- `sessions/store` remains source of truth for session discovery/listing metadata consumed by `af sessions`.
- Writer ownership:
  - spawn path writes `remote_spawn_store`.
  - session discovery (via plugin event → `claimSession`) writes `session_id` mapping in both stores.
  - `af sessions` is read-only over both stores and must not mutate lifecycle state.

Session status mapping for CLI:

- remote `requested|spawning|unknown` -> sessions status `pending`
- remote `running` -> sessions status `active`
- remote `failed|terminated` -> sessions status `inactive`

Provider boundary:

- `SpritesClient` is a concrete Sprites API client used by the spawn command.
- No provider interface — there is only one provider (Sprites) and we don't abstract what doesn't vary.
- If a second provider is added later, extract an interface then.

## Implementation Plan

### Phase 1: Sprites spawn contract and config ✅ DONE

- Add Sprites MVP config surface and credential loading with secure defaults.
- Add explicit provider selection for spawn (`sprites` only in MVP).
- Add strict validation for provider config and remote endpoints.
- Add spawn metadata store schema v1 and migration checks.

Implemented in:

- `cmd/af/cmd/spawn.go`
- `internal/daemon/server_url.go`
- `internal/daemon/provider.go` (data types only — no interface needed)
- `internal/daemon/sprites_client.go`

### Phase 2: Spawn state machine + persistence ✅ DONE

- Implement idempotent spawn request with `request_id`.
- Persist remote spawn metadata and transition states deterministically.
- Enforce idempotency conflict behavior for same key + different payload.

Implemented in:

- `cmd/af/cmd/spawn.go`
- `internal/daemon/remote_spawn_store.go`

### Phase 3: Daemon HTTP transport

Migrate the daemon from Unix socket RPC to an HTTP API. This is the prerequisite for remote session discovery — the plugin on a sprite needs to reach the daemon over the network.

- Replace `net.Listen("unix")` with `net/http` server.
- Map existing RPC methods to REST endpoints (see endpoint table above).
- Migrate CLI client from raw socket to `http.Client`.
- Migrate plugin from `net.createConnection(socket)` to `fetch(url)`.
- Update daemon config: replace `SocketPath` with `ListenAddr` (e.g. `:7070`).
- Update `AETHERFLOW_SOCKET` env var to `AETHERFLOW_URL`.
- Keep the same request/response JSON shapes where possible to minimize churn.

Suggested implementation files:

- `internal/daemon/daemon.go` (listener, connection handling → http.Server)
- `internal/daemon/http.go` (new — HTTP handler/router)
- `internal/daemon/config.go` (SocketPath → ListenAddr)
- `internal/client/client.go` (Unix dial → http.Client)
- `internal/install/plugins/aetherflow-events.ts` (socket → fetch)
- `cmd/af/cmd/root.go` (socket path resolution → URL resolution)

### Phase 4: Attach + session discovery over HTTP

Wire `af session attach` for Sprites-backed sessions and enable session discovery through the HTTP event pipeline.

- Ensure `af sessions` and `af session attach` include Sprites-backed sessions.
- `claimSession` works unchanged — plugin events now arrive over HTTP instead of Unix socket.
- Define deterministic attach output for `requested|spawning|unknown` states (human + JSON modes).

Suggested implementation files:

- `cmd/af/cmd/sessions.go`
- `internal/daemon/event_rpc.go` (adapt to HTTP handler)

### Phase 5: Observability + docs

- Add structured lifecycle logs/metrics and clear CLI error messages.
- Update README/docs for Sprites-first remote spawn usage and escape hatches.

Suggested implementation files:

- `cmd/af/cmd/logs.go`
- `README.md`

## Acceptance Criteria

### Functional

- [x] User can start a remote spawn on Sprites from `af spawn` and receive a stable `spawn_id` handle immediately.
- [ ] `af sessions` lists Sprites-backed sessions with enough info to attach.
- [ ] `af session attach <session-id>` succeeds for active Sprites-backed sessions.
- [ ] `af session attach <spawn-id>` resolves and attaches once mapped session is running.
- [ ] Users can still use Sprites-native CLI/API directly without Aetherflow interference.
- [x] `af session attach` returns deterministic pending/unknown response for non-attachable states and never silently succeeds/fails.

### Transport

- [ ] Daemon listens on HTTP instead of Unix socket.
- [ ] CLI communicates with daemon over HTTP.
- [ ] Plugin sends events to daemon over HTTP (supports `AETHERFLOW_URL`).
- [ ] Session discovery works for remote spawns via plugin HTTP events.

### Security / Trust

- [ ] Only trusted remote endpoint patterns are accepted by default; insecure endpoints are rejected with actionable errors.
- [ ] Sprites token values are never printed in logs, status output, or error chains.
- [ ] TLS hostname and certificate validation failures are surfaced with stable error categories.
- [ ] Private/link-local endpoint targets are rejected by default unless explicitly allowlisted.

### Resilience / Correctness

- [x] Duplicate spawn submissions with same idempotency key do not create duplicate remote runtimes.
- [ ] Same idempotency key with different payload fails fast with conflict and creates no runtime.
- [ ] Partial failure paths (remote success/local persist failure and inverse) converge to one terminal state.
- [ ] Concurrent spawn attempts for the same logical request converge to one runtime + one canonical record.

### Quality Gates

- [ ] Add integration tests for happy path, auth failure, and timeout/retry.
- [ ] Add regression tests for existing local spawn/session flows.
- [ ] Add JSON contract tests for `af sessions` and `af session attach` pending/unknown/error states.

## Success Metrics

- Spawn success rate (Sprites) >= 95% in staging verification runs.
- Attach success rate for Sprites-backed sessions.
- Duplicate spawn rate (should approach zero with idempotency).
- No security regression in endpoint/token handling.

## Dependencies & Risks

### Dependencies

- Sprites API availability and token provisioning.
- Existing daemon/plugin/session registry infrastructure.

### Risks

- **Remote URL trust regression:** relaxing localhost-only validation may introduce security risk if trust policy is weak.
- **Cost/resource leaks:** failed local persistence after remote spawn can leak remote sandboxes.
- **State divergence:** users using provider-native escape hatches can drift local metadata.
- **API drift:** provider API changes can break spawn assumptions.
- **Plugin unreachability:** if the plugin on a sprite can't reach the daemon over HTTP, session discovery fails silently and the spawn stays in `spawning`.

## Risk Mitigation

- Strict trust policy defaults with explicit opt-ins for any non-standard behavior.
- Idempotency key and deterministic state transitions.
- Redaction checks in tests for credentials/errors.
- Explicit retry matrix and `Retry-After` handling tests.
- Plugin retry with backoff for HTTP event delivery failures.

## References

### Internal

- `docs/brainstorms/2026-02-20-remote-sandbox-spawn-brainstorm.md`
- `cmd/af/cmd/spawn.go:61`
- `cmd/af/cmd/sessions.go:339`
- `internal/daemon/server_url.go:25`
- `internal/daemon/daemon.go:347`
- `internal/install/plugins/aetherflow-events.ts:56`
- `internal/sessions/store.go:120`
- `README.md:648`
- `docs/solutions/security-issues/path-traversal-validation-pattern-20260210.md`
- `docs/solutions/security-issues/daemon-cross-project-shutdown-socket-isolation-20260207.md`
- `docs/solutions/runtime-errors/nil-pointer-status-handler-runner-not-set-20260207.md`
- `docs/solutions/runtime-errors/orphaned-in-progress-tasks-reclaim-on-startup-20260207.md`

### External

- `https://docs.sprites.dev/`
- `https://docs.sprites.dev/cli/authentication/`
- `https://docs.sprites.dev/working-with-sprites/`
- `https://sprites.dev/api/sprites`
- `https://sprites.dev/api/sprites/exec`
- `https://sprites.dev/api/sprites/proxy`
- `https://google.aip.dev/155`
- `https://cheatsheetseries.owasp.org/cheatsheets/Transport_Layer_Security_Cheat_Sheet.html`
- `https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html`
