---
date: 2026-02-20
topic: remote-sandbox-spawn
---

# Remote Sandbox Spawn Brainstorm

## What We're Building

We want Aetherflow to spawn agents in a remote sandbox environment, not necessarily local. For MVP, the goal is intentionally narrow: launch an agent remotely and make it reachable. Users can then interact with the sandbox provider directly for most follow-on operations.

The main user outcomes are:
- Start an agent somewhere remote from `af`.
- Attach to the OpenCode session from `af` (existing session model).
- Optionally inspect output artifacts via provider-native mechanisms (for now), rather than AF-owned wrappers.

## Why This Approach

We considered an adapter-heavy design and a simpler spawn-only approach. We are choosing spawn-only for MVP because adapter/wrapper layers are likely premature and can become an anti-pattern if they obscure provider-native capabilities.

This keeps Aetherflow focused on orchestration entrypoint, while preserving user freedom to use provider APIs/CLIs directly. It also reduces early design lock-in while we learn real-world usage patterns for ports, previews, logs, and desktop/web views.

## Key Decisions

- **MVP scope is spawn-first:** Aetherflow should primarily support spawning remote agents and attaching to sessions.
- **No provider abstraction layer in MVP:** Do not build custom adapters/wrappers yet.
- **Escape hatches are required:** Users should be able to interact directly with provider APIs/CLI and not depend on `af` for every action.
- **Attachment priority:** OpenCode session attach is first-class; web/app/CLI inspection should initially rely on provider-native workflows.
- **End-state direction remains BYO infra:** Long-term goal is allowing users to bring their own provider without AF forcing one platform.
- **MVP provider choice:** Start with Sprites as the first remote sandbox provider.

## Daemon Transport Decisions (2026-02-21)

During implementation, we discovered the daemon transport is a prerequisite for remote spawn support. The plugin on a remote sprite can't reach a Unix socket on the user's machine.

Key decisions:

- **HTTP replaces Unix socket entirely.** One transport, not two. Local and remote use the same protocol.
- **No auth for MVP.** Bearer token comes later. Rely on localhost binding for now.
- **Daemon is eventually a hosted service.** Not just a local process — it's a centralized coordination point that sprites, CLIs, and agents all talk to.
- **No reconcile loop needed.** Session discovery works through plugin events over HTTP. The existing `claimSession` mechanism works unchanged — only the transport changes.
- **Polling for log streaming for now.** `af logs -f` polls every 500ms. SSE can be added later as an optimization.
- **No provider interface.** `SpritesClient` is concrete. Extract an interface only when a second provider is added.

## Open Questions

- What objective criteria should trigger adding a second provider after Sprites (cost, reliability, feature gaps)?
- When should bearer token auth be added to the daemon HTTP API?
- Should the daemon URL be user-configurable or auto-discovered?

## Next Steps

-> Plan updated: `docs/plans/2026-02-20-feat-sprites-first-remote-agent-spawn-plan.md`
