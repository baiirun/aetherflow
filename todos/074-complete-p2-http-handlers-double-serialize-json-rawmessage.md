---
status: pending
priority: p2
issue_id: "074"
tags: [code-review, performance, architecture]
dependencies: []
---

# HTTP handlers double-serialize through json.RawMessage

## Problem Statement

`internal/daemon/http.go:65-76` and similar — HTTP handlers decode the request body into a typed struct, re-marshal it to `json.RawMessage`, then pass it to a handler which unmarshals again. This results in 3 serialization passes per request on the hottest path (event ingestion).

## Findings

- Request body is decoded into a typed Go struct via `json.Decoder`.
- The typed struct is then re-marshaled into `json.RawMessage` to match the handler signature.
- The handler then unmarshals the `json.RawMessage` back into a typed struct.
- This triple-serialization is unnecessary overhead on every request, especially on the event ingestion path which is the highest-traffic endpoint.
- The pattern exists across at least 5 HTTP handlers.

## Proposed Solution

- Refactor handler method signatures to accept typed params directly instead of `json.RawMessage`.
- HTTP layer decodes once into the typed struct and passes it directly to the handler.
- Eliminates the double-serialize round-trip in 5 handlers.

## Acceptance Criteria

- [ ] Handler methods accept typed parameter structs instead of `json.RawMessage`.
- [ ] Request bodies are decoded exactly once per request.
- [ ] All 5 affected handlers are updated.
- [ ] Existing tests pass without modification (or with minimal signature updates).
- [ ] No `json.RawMessage` remains in the handler dispatch path.

## Work Log

- (none yet)
