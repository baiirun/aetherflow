---
module: internal/harness
date: 2026-02-10
problem_type: security
symptoms:
  - "User-provided names used directly in filepath.Join() without validation"
  - "Potential for path traversal with ../ sequences"
  - "Risk of writing files outside intended directories"
root_cause: Missing input validation on component names before path construction
severity: critical
tags: [security, path-traversal, input-validation, filesystem]
---

# Path Traversal Prevention Pattern

## Problem

When constructing filesystem paths from user input, failing to validate the input can allow path traversal attacks. An attacker can provide names like `../../../etc/passwd` to escape the intended directory.

**Example vulnerable code:**
```go
func (h *harness) SkillPath(name string) (string, error) {
    // VULNERABLE: name is not validated
    return filepath.Join(h.configDir, "skills", name, "SKILL.md"), nil
}

// Attack:
path, _ := h.SkillPath("../../../tmp/malicious")
// Returns: ~/.config/opencode/skills/../../../tmp/malicious/SKILL.md
// Resolves to: /tmp/malicious/SKILL.md
```

## Root Cause

User-provided strings are concatenated into filesystem paths without validation. Even though `filepath.Join()` normalizes paths, it doesn't prevent traversal - `..` segments are valid path components.

## Solution

Validate all user-provided path components before use. The validation must:

1. Check for empty strings
2. Reject path separators (`/`, `\\`)
3. Reject special directory names (`.`, `..`)
4. Enforce length limits (filesystem typically has 255-char name limit)
5. Use `filepath.Base()` as final safety check

**Implementation:**
```go
func validateComponentName(name string) error {
    if name == "" {
        return errors.New("name cannot be empty")
    }
    
    if len(name) > 255 {
        return errors.New("name too long (max 255 characters)")
    }
    
    // Prevent path traversal - name must not contain path separators
    if strings.Contains(name, "/") || strings.Contains(name, "\\") {
        return fmt.Errorf("name cannot contain path separators: %q", name)
    }
    
    // Reject special directory names
    if name == "." || name == ".." {
        return fmt.Errorf("invalid name: %q", name)
    }
    
    // Use filepath.Base as additional safety check
    if filepath.Base(name) != name {
        return fmt.Errorf("name contains invalid path components: %q", name)
    }
    
    return nil
}

func (h *harness) SkillPath(name string) (string, error) {
    if err := validateComponentName(name); err != nil {
        return "", fmt.Errorf("invalid skill name: %w", err)
    }
    return filepath.Join(h.configDir, "skills", name, "SKILL.md"), nil
}
```

## Prevention Strategy

**Always validate at the boundary:**
- Parse user input into validated types once when it enters the system
- Don't scatter validation checks throughout the codebase

**Defense in depth:**
1. Input validation (this pattern)
2. Use `filepath.Base()` even after validation
3. Avoid constructing paths from multiple user inputs
4. Consider allowlisting valid characters if applicable

**Test coverage:**
```go
func TestPathTraversal(t *testing.T) {
    tests := []string{
        "../../../etc/passwd",
        "foo/bar",
        "/absolute/path",
        ".",
        "..",
        "",
    }
    
    h := NewHarness()
    for _, name := range tests {
        _, err := h.SkillPath(name)
        if err == nil {
            t.Errorf("SkillPath(%q) should return error", name)
        }
    }
}
```

## Related Patterns

This same pattern applies whenever constructing paths from user input:
- File upload handlers (validating filenames)
- Template rendering (validating template names)
- Static file servers (validating requested paths)
- Archive extraction (validating entry names in zip/tar files)

## See Also

- CWE-22: Improper Limitation of a Pathname to a Restricted Directory
- OWASP Path Traversal
- `internal/protocol/socket.go` uses this pattern for socket path validation
