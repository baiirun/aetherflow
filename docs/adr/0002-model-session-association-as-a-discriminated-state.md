# Model Session Association as a discriminated state

A Session is always either standalone or associated with exactly one Channel,
so Aetherflow represents the relationship as the `SessionAssociation`
discriminated union rather than an optional `channel_id`. The explicit states
make missing data unambiguous and prevent Channel association from being
conflated with Agent participation or public Session visibility.
