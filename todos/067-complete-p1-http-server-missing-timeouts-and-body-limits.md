---
status: pending
priority: p1
issue_id: "067"
tags: [code-review, security, performance]
dependencies: []
---

# HTTP server missing timeouts and body size limits (DoS vector)

## Problem Statement

`internal/daemon/daemon.go:132-136` creates `http.Server` with no `ReadTimeout`, `WriteTimeout`, `ReadHeaderTimeout`, or `MaxHeaderBytes`. POST handlers in `http.go` decode `r.Body` without size limits. A slowloris attack can exhaust connections indefinitely; a large POST body can exhaust memory.

## Findings

- All 3 security/perf/tigerstyle reviewers flagged this independently.
- The `http.Server` struct is initialized with zero-value timeouts, meaning no deadline is enforced on reads, writes, or header parsing.
- POST handlers pass `r.Body` directly to JSON decoders with no upper bound on request size.
- This is a well-known DoS vector: slowloris holds connections open indefinitely, and unbounded body reads can exhaust process memory.

## Proposed Solution

1. Add timeouts to the `http.Server` initialization in `daemon.go`:
   - `ReadTimeout: 30 * time.Second`
   - `WriteTimeout: 30 * time.Second`
   - `ReadHeaderTimeout: 10 * time.Second`
   - `MaxHeaderBytes: 1 << 20` (1 MB)
2. Wrap POST handler bodies with `http.MaxBytesReader(w, r.Body, 1<<20)` before decoding.

**Effort:** Small (30 min)

## Acceptance Criteria

- [ ] `http.Server` has all 4 timeout/limit fields set (`ReadTimeout`, `WriteTimeout`, `ReadHeaderTimeout`, `MaxHeaderBytes`).
- [ ] All POST handlers use `http.MaxBytesReader` to cap body size.
- [ ] Test that an oversized POST body returns HTTP 413 (Request Entity Too Large).

## Work Log
