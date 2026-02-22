---
status: pending
priority: p2
issue_id: "079"
tags: [code-review, architecture]
dependencies: []
---

# writeResponse maps all failures to HTTP 400

## Problem Statement

`internal/daemon/http.go:57-63` — All error responses return 400 Bad Request regardless of whether the error is a client error (bad input), server error (internal failure), or not-found. This makes the API harder to consume and debug.

## Findings

- `writeResponse` unconditionally uses HTTP 400 for all error cases.
- Client errors (validation failures), server errors (internal panics, downstream failures), and not-found errors all return the same status code.
- Consumers cannot distinguish between "your request was malformed" and "something broke on the server side" without parsing error message strings.
- This violates HTTP semantics and makes the API harder to integrate with, monitor, and debug.

## Proposed Solution

- Add error categorization to the handler layer:
  - 400 Bad Request for validation/input errors.
  - 404 Not Found for missing resources.
  - 500 Internal Server Error for unexpected server-side failures.
- Introduce a typed error (e.g., `HandlerError` with a `StatusCode` field) or use sentinel errors to classify error categories.
- Update `writeResponse` to extract the appropriate status code from the error.

## Acceptance Criteria

- [ ] Validation/input errors return HTTP 400.
- [ ] Not-found errors return HTTP 404.
- [ ] Internal/unexpected errors return HTTP 500.
- [ ] Error categorization is implemented via typed errors or a similar mechanism (not string matching).
- [ ] Existing error messages are preserved in response bodies.
- [ ] Tests verify correct status codes for each error category.

## Work Log

- (none yet)
