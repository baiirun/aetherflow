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

## Open Questions

- What minimum spawn metadata should AF persist (provider id, sandbox id, session id, preview URL, ports)?
- Should AF include a generic passthrough command for provider CLI/API calls, or remain fully hands-off after spawn?
- What baseline security model is required for credentials and remote endpoints in config?
- What objective criteria should trigger adding a second provider after Sprites (cost, reliability, feature gaps)?

## Next Steps

-> `/workflows:plan` to define the implementation path for spawn-only remote orchestration.
