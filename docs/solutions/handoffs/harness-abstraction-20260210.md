# Harness Abstraction Implementation

**Task**: ts-555e54  
**Date**: 2026-02-10  
**Status**: Reviewing (PR #3)

## What Was Done

Implemented a harness abstraction for skill/agent/plugin installation across different agent runtimes (opencode, claude, codex). The abstraction provides:

1. **Harness interface** with methods for path resolution (`SkillPath`, `AgentPath`, `PluginPath`)
2. **Capability detection** (`SupportsSkills()`, `SupportsAgents()`, `SupportsPlugins()`)
3. **Plugin registration hook** for harnesses requiring config file updates
4. **System detection** to check if a harness is installed
5. **Input validation** to prevent path traversal attacks

## Implementation Details

**Files created:**
- `internal/harness/harness.go` - Core interface and implementations
- `internal/harness/harness_test.go` - Comprehensive test coverage

**Key components:**
- `validateComponentName()` - Security validation preventing path traversal, empty names, special chars
- `detectHarness()` - Shared detection logic (checks PATH and config directory)
- Three harness implementations:
  - `openCodeHarness` - Full support for skills/agents/plugins in `~/.config/opencode/`
  - `claudeHarness` - Skills and agents in `~/.claude/`, plugins not supported
  - `codexHarness` - Stub returning `ErrNotSupported` for all operations

## Original Requirements (DoD)

From task description:
- ✅ Harness type/interface that resolves skill, agent, and plugin installation directories
- ✅ Per-category support flags (harness can say "I don't support plugins")
- ✅ opencode harness returns correct paths for all three categories
- ✅ claude harness returns correct paths for skills and agents, "not supported" for plugins
- ✅ codex returns a descriptive error
- ✅ Detection function per harness for interactive mode pre-selection
- ✅ Unit tests verify path resolution and detection for each harness

## What Was Tried That Didn't Work

Initial implementation had several P1 security and correctness issues identified during review:

1. **Path traversal vulnerability**: Original implementation didn't validate `name` parameters. An attacker could pass `../../../etc/passwd` to escape the intended directory. Fixed by adding `validateComponentName()` that checks for path separators, special names, and length limits.

2. **Silent error swallowing**: Used `home, _ := os.UserHomeDir()` which ignored errors. If `UserHomeDir()` failed, paths would be constructed with empty string leading to `/.config/opencode`. Fixed by checking error and panicking if home directory cannot be determined.

3. **No-op RegisterPlugin**: Initially returned `nil` with a comment saying "will be implemented later". This violated the interface contract - callers couldn't distinguish between "registration succeeded" and "not yet implemented". Fixed by returning `ErrNotSupported` until actual registration logic is built.

4. **Duplicate detection logic**: Same pattern repeated across all three harnesses. Extracted to `detectHarness()` helper function.

5. **Inconsistent error handling**: Codex returned plain strings, claude wrapped sentinel errors. Made all harnesses consistently wrap `ErrNotSupported` for proper `errors.Is()` checking.

## Key Decisions

1. **Panic on UserHomeDir failure** - Chosen over returning errors from constructors because home directory is fundamental to the abstraction. If we can't determine home, the harness is unusable. Panicking makes this failure immediate and obvious.

2. **Keep SupportsX() methods** - Review suggested removing these as redundant (could check if methods return `ErrNotSupported`). Kept them because they allow capability checking without attempting operations, which is cleaner for UI flows like "show only supported harnesses in picker".

3. **Keep Codex stub** - Review suggested removing until needed (YAGNI). Kept it because:
   - The DoD explicitly requires it
   - `All()` function can return a complete set
   - Adding it later would be a breaking change if consumers depend on the harness set

4. **Validation in path methods** - Could have validated at a higher layer (install command), but putting it in the harness ensures all consumers get protection and the harness can't produce invalid paths.

## Test Coverage

Comprehensive tests including:
- Happy path for all harnesses and all path types
- Validation of malicious inputs (path traversal, empty strings, special chars)
- Error wrapping verification with `errors.Is()`
- `All()` and `Detected()` helper functions
- RegisterPlugin behavior for all harnesses

All tests passing, build successful.

## Integration Point

This package is currently standalone - no other code imports it yet. The next task (`ts-ea281b` - implement `af install` command) will consume this abstraction.

Expected usage:
```go
import "github.com/geobrowser/aetherflow/internal/harness"

// Detect installed harnesses
for _, h := range harness.Detected() {
    if h.SupportsSkills() {
        skillPath, err := h.SkillPath("review-auto")
        // Copy skill file to skillPath
    }
}
```

## Remaining Work

None for this task - complete and ready to merge. Follow-on work in the epic:
- `ts-ea281b` - Implement `af install` command using this abstraction
- `ts-ccd9be` - Add install verification to daemon startup
- Plugin registration logic when `RegisterPlugin` is actually needed

## Known Limitations

1. **RegisterPlugin is a stub** - Returns `ErrNotSupported` until implementation needed
2. **Detection is best-effort** - TOCTOU race between detection and use (documented in review)
3. **No version handling** - All paths are unversioned (e.g., no `~/.config/opencode/v2/`)
4. **Codex paths unknown** - Stub implementation until research task completes
