---
status: complete
priority: p2
issue_id: "046"
tags: [code-review, safety, reliability]
dependencies: []
---

# Add size limit for event payloads in handleSessionEvent

## Problem Statement

`handleSessionEvent` stores `SessionEvent.Data` (a `json.RawMessage` of arbitrary length) from the Unix socket with no size check. A single event with a multi-megabyte Data field will be stored in the ring buffer. Combined with unbounded sessions (issue 045), this could lead to excessive memory usage.

## Findings

- `SessionEventParams.Data` is `json.RawMessage` — no size validation
- Stored directly into EventBuffer via Push()
- With 2000 events per session × unbounded payload × unbounded sessions, memory is effectively uncapped
- Typical events are 500B-2KB, but tool output events can be much larger
- Flagged by tigerstyle-reviewer agent

## Proposed Solutions

### Option 1: Reject oversized events at RPC boundary (Recommended)

**Approach:** Add a `maxEventDataBytes` constant (256KB) and reject events exceeding it.

**Pros:** Simple, enforced at boundary, clear error message
**Cons:** Large tool outputs would be silently dropped
**Effort:** 10 minutes
**Risk:** Low

### Option 2: Truncate oversized Data field

**Approach:** Truncate `Data` to max size instead of rejecting.

**Pros:** No data loss for the event metadata
**Cons:** Truncated JSON is unparseable downstream
**Effort:** 15 minutes
**Risk:** Medium (downstream parse failures)

## Recommended Action



## Acceptance Criteria

- [ ] Events with oversized Data are handled (rejected or truncated)
- [ ] Normal events (< 256KB) are unaffected
- [ ] Test verifies size limit enforcement

## Work Log

### 2026-02-19 - Code Review Discovery

**By:** Claude Code (tigerstyle-reviewer)

**Actions:**
- Identified missing size limit on event payloads
- Estimated typical vs worst-case payload sizes
