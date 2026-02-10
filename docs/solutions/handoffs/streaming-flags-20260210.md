# Streaming Flags Unification

**Task**: ts-e73941  
**Date**: 2026-02-10  
**PR**: https://github.com/baiirun/aetherflow/pull/1

## Problem

The CLI had inconsistent flags for continuous output:
- `af status` used `--watch/-w` (poll-and-refresh loop)
- `af logs` used `--follow/-f` (tail-follow mode)

Users had to remember which flag worked with which command, creating unnecessary friction.

## Solution

Implemented Option C from the task description: support both flags as aliases on both commands.

- `status.go`: Added `--follow/-f` as alias for `--watch`
- `logs.go`: Added `--watch/-w` as alias for `--follow`
- Both flags are ORed together in the Run function to enable streaming
- Help text updated to clearly indicate the alias relationship

## Implementation Details

### Changed Files

1. **cmd/af/cmd/status.go**
   - Added `--follow/-f` flag registration in init()
   - Modified Run to check `watch || follow` instead of just `watch`
   - Updated help text to mention both flags
   - Updated error message for JSON incompatibility

2. **cmd/af/cmd/logs.go**
   - Added `--watch/-w` flag registration in init()
   - Modified Run to check `follow || watch` instead of just `follow`
   - Updated help text to mention both flags

3. **cmd/af/cmd/status_test.go**
   - Added TestStatusFlagAliases to verify both flags work
   - Updated TestStatusFlagsRegistered to check for new flags

4. **cmd/af/cmd/logs_test.go**
   - Added TestLogsFlagAliases to verify both flags work
   - Added TestLogsFlagsRegistered to check for new flags

### Design Decisions

**Why not synchronize flags?** The flags don't need to be synchronized at the Cobra flag level. What matters is that the Run function ORs them together, so either flag (or both) triggers streaming behavior. This is simpler than trying to make Cobra sync them.

**Why show both in help?** Users familiar with `kubectl -w` or `tail -f` should be able to use their muscle memory. Showing both flags makes it clear that either works.

**Alias direction**: The help text indicates which is the "primary" flag (the one that was there first) and which is the alias, but functionally they're equivalent.

## Verification

- All existing tests pass
- New tests verify flag registration and functionality
- `go vet` passes
- Binary builds successfully
- Help text is consistent across commands
- No breaking changes - all existing flags continue to work

## What Didn't Work

Nothing - this was a straightforward implementation. First approach worked.

## Remaining Work

None. Feature is complete and ready for review.
