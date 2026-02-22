---
status: pending
priority: p2
issue_id: "073"
tags: [code-review, simplicity, yagni]
dependencies: []
---

# Remove dead code: Provider interface, GetStatus, Terminate, unused types

## Problem Statement

Multiple unused constructs exist across the codebase: `Provider` interface (`provider.go:38-43`) defined but never used; `GetStatus()` and `Terminate()` on SpritesClient (65 lines) never called; `ProviderStatusResult`, `ProviderRuntimeWarm/Cold` types only used by dead methods; `CanonicalName` field set but never read; `LastReconciledAt` field never written; `daemonURLToListenAddr` in http.go duplicates `listenAddrFromURL` in config.go; `Request` struct in daemon.go vestigial from removed socket dispatch.

## Findings

- `Provider` interface is defined but has zero implementations or consumers.
- `GetStatus()` and `Terminate()` methods on `SpritesClient` are never called anywhere in the codebase.
- `ProviderStatusResult`, `ProviderRuntimeWarm`, and `ProviderRuntimeCold` types exist solely to support the dead `GetStatus` method.
- `CanonicalName` field is assigned but never read by any consumer.
- `LastReconciledAt` field is declared but never written to.
- `daemonURLToListenAddr` in `http.go` duplicates `listenAddrFromURL` in `config.go`.
- `Request` struct in `daemon.go` is a vestige from a removed socket dispatch mechanism.
- Total: ~250 lines of removable dead code.

## Proposed Solution

- Delete the `Provider` interface and all associated unused types.
- Delete `GetStatus()` and `Terminate()` methods from `SpritesClient`.
- Delete `ProviderStatusResult`, `ProviderRuntimeWarm`, `ProviderRuntimeCold` types.
- Remove the `CanonicalName` field and all assignments to it.
- Remove the `LastReconciledAt` field.
- Remove `daemonURLToListenAddr` and consolidate on `listenAddrFromURL`.
- Remove the vestigial `Request` struct from `daemon.go`.

## Acceptance Criteria

- [ ] All listed dead code is removed (~250 lines).
- [ ] Build passes with no compilation errors.
- [ ] All existing tests pass.
- [ ] No references to removed symbols remain in the codebase.

## Work Log

- (none yet)
