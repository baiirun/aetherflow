# Invariant Registry

These identifiers provide stable names for architectural constraints. A change
that intentionally breaks one MUST update this registry and either supersede the
relevant ADR or explicitly describe why no decision record is needed.

## Domain

| ID | Invariant |
| --- | --- |
| `DOM-1` | A Session is private to one Agent even when associated with a Channel. |
| `DOM-2` | Session Association is a discriminated state: standalone or exactly one Channel. |
| `DOM-3` | Agent identity, Channel identity, and Session identity remain separate. |

## Identity and runtime

| ID | Invariant |
| --- | --- |
| `ID-1` | One `SessionId` identifies the domain Session, its Rivet actor key, and its persistent Pi session. |
| `ID-2` | Rivet actor IDs, daemon PIDs, and Pi PIDs are runtime identities and never replace `SessionId`. |
| `RUN-1` | A durable Session actor always uses persistent Pi storage. |
| `RUN-2` | One active Session actor owns one headless Pi RPC subprocess. |
| `RUN-3` | The Pi subprocess is replaceable; actor state, Pi continuation, and event history survive its replacement. |
| `RUN-4` | Closing the desktop does not terminate its daemon, allowing detached turns and CLI reconnection. |
| `RUN-5` | A daemon is reusable across package versions only when its daemon protocol is compatible. |

## Events and observation

| ID | Invariant |
| --- | --- |
| `EVT-1` | A Session Event is persisted before live publication. |
| `EVT-2` | Event sequence is positive, monotonic, and local to one Session. |
| `EVT-3` | Cursor reads are exclusive and bounded. |
| `EVT-4` | Replay-to-live observation subscribes before replay and removes overlap by sequence. |
| `EVT-5` | Pi's raw JSON fields are preserved even when a typed union variant is available or the event kind is unknown, except for deliberate Attachment-byte externalization. |

## Discovery

| ID | Invariant |
| --- | --- |
| `DIR-1` | Listing Sessions reads the Session Directory rather than waking every Session actor. |
| `DIR-2` | The Session Directory owns navigation metadata, not conversation history or Pi runtime state. |
| `DIR-3` | Archiving changes discovery metadata without deleting Session authorities. |

## Attachments

| ID | Invariant |
| --- | --- |
| `ATT-1` | Actor commands and persisted events carry Attachment references rather than binary bytes. |
| `ATT-2` | Attachment identity is the SHA-256 digest of its bytes, and reads verify digest and length. |
| `ATT-3` | Base64 image data exists only at Pi protocol and display hydration seams, not in durable Aetherflow event payloads. |

## Distribution

| ID | Invariant |
| --- | --- |
| `DIST-1` | Installing `aetherflow-desktop` installs both the desktop executable and `aetherflowd` from one Cargo package transaction. |
| `DIST-2` | The macOS app bundle carries `aetherflowd` as a signed helper and prefers it over `PATH`. |
| `DIST-3` | `af` remains a separately installable CLI that uses the same client contract. |

## Explicit non-invariants

The following properties are intentionally not guaranteed today:

- There is no global sequence shared by all Sessions.
- Session actor creation and Session Directory registration are not one atomic
  transaction.
- Session Events are not the authoritative Pi continuation format.
- A Session descriptor is not a complete Session snapshot.
- Local and hosted writers cannot currently synchronize or share authority.
- Modeled Agent and Channel relationships are not yet complete product flows.

## Verification by group

- `DOM-*` and `ID-*`: domain serialization tests and Session actor configuration
  tests.
- `RUN-*`: Session actor integration tests, daemon readiness tests, and
  `scripts/smoke-session-lifecycle.sh`.
- `EVT-*`: Session actor cursor tests, Pi protocol round-trip tests, and the
  ignored Rivet replay-to-live integration test.
- `DIR-*`: Session Directory descriptor and client lifecycle tests.
- `ATT-*`: Attachment store, actor-message-size, and attachment HTTP tests.
- `DIST-*`: Cargo target metadata, isolated `cargo install`, and
  `scripts/build-macos-app.sh` signature verification.
