# Session Promotion

Status: future goal. Current development should implement and validate local
Rivet-backed sessions before adding promotion.

## Purpose

Aetherflow should be able to begin a Session entirely on a user's machine, using
Rivet's local filesystem persistence, and later continue that same logical
Session on a hosted Rivet deployment. Local-first use must not create a second
data model or prevent a Session from becoming remotely accessible later.

## Scope

Session Promotion transfers authoritative execution and durable Session data
from a local Rivet deployment to a hosted Rivet deployment.

It does not provide:

- transparent migration of a running Pi process;
- continuous bidirectional or multi-writer synchronization;
- copying of Rivet's internal storage files;
- promotion of every Agent or Channel associated with the Session; or
- compatibility between arbitrary versions of the Session actor.

## Target Behavior Contract

- A promoted Session MUST retain its Aetherflow `SessionId`.
- The local and hosted actors MUST NOT be authoritative writers for the Session
  at the same time.
- The local actor MUST stop accepting mutations before it captures the
  promotion bundle.
- The local actor MUST remain authoritative until the hosted actor has durably
  accepted the bundle.
- Promotion MUST preserve the ordered durable Session history and sufficient Pi
  continuation data to resume the interaction.
- Promotion MUST NOT treat Rivet's physical actor ID as the Session's identity.
  The hosted actor may have a different physical actor ID while using the same
  `SessionId` as its logical key.
- Import MUST be idempotent. Retrying an interrupted promotion MUST NOT create a
  second logical Session or duplicate imported events.
- A failed promotion MUST leave the local Session resumable.
- After successful promotion, local clients MUST direct mutations to the hosted
  actor. Any retained local data is a cache or recovery copy, not an
  authoritative replica.
- A running Pi subprocess MUST be stopped locally and recreated remotely. Its
  operating-system process state is not part of the promotion bundle.

## Interfaces

### Session promotion bundle

The portable bundle should contain only Aetherflow-owned, versioned data:

- schema version and promotion attempt identifier;
- Session, Agent, and optional Channel identities;
- durable Session events and their last sequence;
- portable Pi continuation material;
- runtime configuration required to resume the Session; and
- integrity metadata for detecting incomplete or corrupt transfers.

Credentials and machine-specific filesystem paths MUST NOT be embedded without
an explicit portability and security policy.

### Promotion coordinator

The desktop app or CLI should coordinate promotion across the two deployments:

1. Ask the local Session actor to quiesce and export a bundle.
2. Address or create the hosted Session actor using the same `SessionId` key.
3. Ask the hosted actor to import and durably validate the bundle.
4. Record the hosted location only after acknowledgement.
5. Either resume locally after failure or redirect clients after success.

Promotion should use explicit actor actions rather than Rivet inspector APIs so
the contract remains independent of Rivet's internal state, queues, schedules,
and embedded database representation.

## Implementation Model

The local and hosted deployments should compile the same Session actor and share
the same domain types and state-transition logic. They differ only in Rivet
endpoint, persistence location, authentication, and process environment.

The first implementation phase is local-only:

- run the Session actor against Rivet's local Engine and filesystem persistence;
- place the Pi subprocess and stream reader in ephemeral actor state;
- keep durable Session identity, lifecycle status, and Pi continuation metadata
  in persisted actor state; and
- prove that a sleeping or restarted actor can recreate Pi and continue.

Promotion is a later orchestration layer over that local contract. It is not a
reason to add export, remote routing, or conflict resolution to the first local
implementation.

## Verification

A future promotion implementation should demonstrate:

1. A local Session accepts prompts and records ordered events.
2. Promotion preserves its `SessionId` and event sequence on the hosted actor.
3. The hosted actor resumes Pi from the exported continuation material.
4. Commands racing with quiescence are rejected or ordered before the snapshot.
5. Repeating the import with the same attempt identifier is harmless.
6. Failure before hosted acknowledgement leaves the local Session usable.
7. Failure after hosted acknowledgement cannot produce two authoritative
   writers.
8. Secrets and machine-local paths are absent from the portable bundle unless
   explicitly supported.

## Open Questions

- What is the minimal portable representation needed to resume a Pi session?
- Should successful promotion retain a local recovery copy, and for how long?
- How are credentials and local tool capabilities replaced in the hosted
  environment?
- Must an associated Channel already exist remotely, or can Session promotion
  create a placeholder association?

## Rivet Notes

As of 2026-08-15, Rivet documents local filesystem persistence and separate
deployment targets, but not a built-in operation that promotes one actor from a
local Engine into a hosted Engine. Actor keys are scoped to an actor type and
deployment, so Aetherflow must preserve logical identity explicitly.

- [Rivet local quickstart](https://rivet.dev/docs/actors/quickstart/backend/)
- [Rivet actor keys](https://rivet.dev/docs/actors/keys/)
- [Rivet persistence](https://rivet.dev/docs/actors/persistence/)
- [Rivet inspector and management interfaces](https://rivet.dev/docs/actors/debugging/)
