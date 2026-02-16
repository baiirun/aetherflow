package daemon

import "embed"

// promptsFS holds the embedded prompt templates compiled into the binary.
// These are the default prompts used when Config.PromptDir is empty.
// Only files passed through RenderPrompt are listed — handoff.md is
// referenced differently (inlined by agents, not rendered by the daemon).
//
//go:embed prompts/worker.md prompts/planner.md prompts/spawn.md
var promptsFS embed.FS
