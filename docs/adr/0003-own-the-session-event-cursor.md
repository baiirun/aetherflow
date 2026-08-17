# Own the Session Event cursor

Pi emits ordered JSONL records but does not provide one resumable sequence for
every live RPC event. Aetherflow therefore persists each Session Event before
publication and allocates a per-Session sequence in the Session actor's SQL
database. Reads use bounded exclusive cursors, and replay-to-live consumers
subscribe before replay and discard overlap by sequence; Pi's canonical payload
shape remains unchanged inside the Aetherflow envelope.
