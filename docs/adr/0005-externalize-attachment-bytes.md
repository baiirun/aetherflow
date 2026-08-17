# Externalize Attachment bytes

Inline image data can exceed Rivet actor-message limits and makes durable event
rows grow with base64 payloads. Aetherflow stores bytes in a content-addressed
local Attachment store and carries only digest, media type, and length through
actor commands and persisted events. Bytes are hydrated only when writing Pi
stdin or rendering a client view and are externalized again before persistence,
trading an additional local HTTP/filesystem read for bounded actor messages and
deduplicated storage.
