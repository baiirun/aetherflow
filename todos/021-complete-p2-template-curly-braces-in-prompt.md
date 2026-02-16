---
status: complete
priority: p2
issue_id: "021"
tags: [code-review, quality]
dependencies: []
---

# Handle `{{` in user prompts gracefully

## Problem Statement

A user prompt containing `{{` (e.g., `"fix the {{config}} parser"`) triggers the unresolved template variable check in `RenderSpawnPrompt` and fails with a cryptic error: "unresolved template variable in embedded". This is a usability bug — the error message doesn't explain that the user prompt contains template-like syntax.

The replacement order (land_steps → land_donts → spawn_id → user_prompt) means user_prompt is replaced last, so `{{` in the prompt survives into the final check. No injection risk — just confusing UX.

## Findings

- Found by: security-sentinel, tigerstyle-reviewer
- Location: `internal/daemon/prompt.go:190-196` (RenderSpawnPrompt unresolved check)
- Not a security issue — the check catches it, but the error is misleading

## Proposed Solutions

### Option 1: Validate user_prompt before substitution (Recommended)

**Approach:** Check for `{{` in the user prompt and return a clear error:

```go
if strings.Contains(userPrompt, "{{") {
    return "", fmt.Errorf("user prompt must not contain '{{' (template syntax)")
}
```

- **Pros:** Clear error message, early detection
- **Cons:** Restricts valid prompts containing `{{` (rare but possible)
- **Effort:** Tiny
- **Risk:** Low

### Option 2: Escape `{{` in user prompt before substitution

**Approach:** Replace `{{` with a placeholder before template substitution, restore after.

- **Pros:** Supports all user prompts including those with `{{`
- **Cons:** More complex, potential for double-escaping bugs
- **Effort:** Small
- **Risk:** Medium

## Recommended Action

Option 1 for now — clear error message. If users hit it frequently, upgrade to Option 2.

## Technical Details

- **Affected files:** `internal/daemon/prompt.go` (RenderSpawnPrompt)

## Acceptance Criteria

- [ ] User prompt with `{{` produces clear error mentioning the user prompt
- [ ] Test for this case

## Work Log

| Date | Action | Learnings |
|------|--------|-----------|
| 2026-02-16 | Created from code review | Existing worker prompt has same issue |
