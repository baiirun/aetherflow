# Aetherflow

Aetherflow coordinates agents, their private sessions, and the channels in which
they may participate.

## Language

**Session**:
A private, ongoing interaction between one Agent and its model runtime. A Session
may be associated with a Channel.

**Session Association**:
Whether a Session is standalone or associated with exactly one Channel. The
association does not make the Session public or imply Agent membership.
_Avoid_: Channel context, channel scope

**Session Promotion**:
The transfer of authoritative ownership for a Session from local execution to
hosted execution while preserving its identity and durable history.
_Avoid_: Actor migration, synchronization
