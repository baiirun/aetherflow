package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Landing instructions injected into the worker prompt based on solo mode.
// These replace {{land_steps}} and {{land_donts}} in worker.md.
const (
	landStepsNormal = `2. **Push your branch** -- from inside your worktree: ` + "`git push -u origin HEAD`" + `. If push fails (no remote configured), that's fine — log the situation and continue. The branch still exists locally for review.
3. **Create PR** -- if push succeeded, create a PR with a clear title and description summarizing the change. If push failed, skip this step.
4. **Clean up worktree** -- remove your worktree: ` + "`git worktree remove .aetherflow/worktrees/{{task_id}}`" + `
5. **Mark task for review** -- ` + "`prog review {{task_id}}`" + `. This signals that your work is complete and awaiting merge. Do NOT use ` + "`prog done`" + ` — the daemon will automatically mark the task done when your branch lands on main.`

	landStepsSolo = `2. **Pull latest main** -- before merging, ensure your local main is up to date:
   ` + "```bash" + `
   git checkout main
   git pull origin main
   ` + "```" + `
   If pull fails (no remote), that's fine — continue with local state.
3. **Merge to main** -- from the project root (NOT the worktree):
   ` + "```bash" + `
   git merge af/{{task_id}} --no-ff -m "Merge af/{{task_id}}: <brief summary>"
   ` + "```" + `
   If the merge has conflicts, try to resolve them. If conflicts are too complex to resolve cleanly, abort and yield:
   ` + "```bash" + `
   git merge --abort
   prog block {{task_id}} "Merge conflicts with main require manual resolution"
   ` + "```" + `
   Then stop — do not continue with further steps.
4. **Push main** -- ` + "`git push origin main`" + `. If push fails (no remote), that's fine — the merge is local.
5. **Clean up** -- remove the branch and worktree:
   ` + "```bash" + `
   git worktree remove .aetherflow/worktrees/{{task_id}}
   git branch -d af/{{task_id}}
   ` + "```" + `
6. **Mark task done** -- ` + "`prog done {{task_id}}`" + `. In solo mode the merge already landed, so the task is complete.`

	landDontsNormal = `- Don't merge your PR -- just create it. The daemon moves tasks to done when branches land on main.
- Don't use ` + "`prog done`" + ` -- use ` + "`prog review`" + ` instead. The done transition happens automatically after merge.`

	landDontsSolo = `- Don't leave your branch unmerged -- in solo mode you are responsible for merging to main.
- Don't forget to delete the branch after merging -- clean up after yourself.`

	// Runtime-conditional content for worker.md.
	// These replace {{context_comment}}, {{review_instructions}}, and {{compound_instructions}}.

	contextCommentOpencode = `<!-- Machine-parsed by .opencode/plugins/compaction-handoff.ts — do not change this format -->`
	contextCommentClaude   = ``

	reviewInstructionsOpencode = `Load ` + "`skill: review-auto`" + `. It will guide you through spawning parallel review subagents on your diff and collecting prioritized findings.`
	reviewInstructionsClaude  = `Run parallel code reviewers on your current changes. Each reviewer gets a fresh context.

1. Get the list of changed files: ` + "`git diff --stat $(git merge-base HEAD main)..HEAD`" + `
2. Spawn these Task agents **simultaneously** (they are independent):

` + "```" + `
Task(subagent_type="general-purpose", prompt="You are a code reviewer. Review code changes for bugs, correctness, and logic errors.

Context: <what this change does>
Changed files: <git diff --stat output>

Run ` + "`git diff $(git merge-base HEAD main)..HEAD`" + ` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are a code simplicity reviewer. Review code changes for unnecessary complexity and simplification opportunities.

Context: <what this change does>
Changed files: <git diff --stat output>

Run ` + "`git diff $(git merge-base HEAD main)..HEAD`" + ` to see the full diff. Return findings as P1/P2/P3.")

Task(subagent_type="general-purpose", prompt="You are a security reviewer. Review code changes for security vulnerabilities.

Context: <what this change does>
Changed files: <git diff --stat output>

Run ` + "`git diff $(git merge-base HEAD main)..HEAD`" + ` to see the full diff. Return findings as P1/P2/P3.")
` + "```" + `

3. Collect and deduplicate findings from all reviewers. Keep the highest severity for duplicates. Discard any reviewer that returned an error or empty results.
4. Return findings as P1 (must fix), P2 (should fix), P3 (consider).`

	compoundInstructionsOpencode = `7. **Compound** -- load ` + "`skill: compound-auto`" + `. It will guide you through documentation enrichment, feature matrix updates, learnings, and handoff.`
	compoundInstructionsClaude  = `7. **Compound** -- capture and persist what this task produced beyond the code:
   - If this was non-trivial work (multiple investigation attempts, non-obvious solution), write a solution doc to ` + "`docs/solutions/<category>/<slug>.md`" + `
   - If ` + "`MATRIX.md`" + ` exists, update coverage for behaviors you implemented
   - Log genuine learnings (non-obvious patterns, pitfalls) to ` + "`docs/solutions/learnings.md`" + `
   - Write a handoff summary to ` + "`docs/solutions/handoffs/<slug>-<YYYYMMDD>.md`" + ` covering what was done, what was tried, key decisions, and remaining concerns
   - Persist handoff to prog: ` + "`prog log {{task_id}} \"Handoff: <summary>\"`" + ``
)

// RenderPrompt reads a role prompt template and replaces template variables
// with actual values.
//
// When promptDir is empty, prompts are read from the embedded filesystem
// compiled into the binary. When promptDir is set, prompts are read from
// that filesystem path instead (for development/customization).
//
// Recognized variables:
//   - {{task_id}} — the task identifier
//   - {{land_steps}} — landing instructions (solo vs normal mode)
//   - {{land_donts}} — "what not to do" rules for landing
//   - {{context_comment}} — runtime-specific header comment (opencode plugin marker or empty)
//   - {{review_instructions}} — runtime-specific review section
//   - {{compound_instructions}} — runtime-specific compound section
//
// Returns the rendered prompt string ready to pass as the message argument
// to the agent runtime.
func RenderPrompt(promptDir string, role Role, taskID string, solo bool, runtime Runtime) (string, error) {
	// Allowlist roles to prevent path traversal if role ever becomes dynamic.
	switch role {
	case RoleWorker, RolePlanner:
	default:
		return "", fmt.Errorf("unknown role: %q", role)
	}

	filename := string(role) + ".md"

	var data []byte
	var err error

	if promptDir == "" {
		// Read from embedded filesystem.
		data, err = fs.ReadFile(promptsFS, "prompts/"+filename)
		if err != nil {
			return "", fmt.Errorf("reading embedded prompt %s: %w", filename, err)
		}
	} else {
		// Read from filesystem override.
		path := filepath.Join(promptDir, filename)
		data, err = os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading prompt %s: %w", path, err)
		}
	}

	// Select landing instructions based on mode.
	landSteps := landStepsNormal
	landDonts := landDontsNormal
	if solo {
		landSteps = landStepsSolo
		landDonts = landDontsSolo
	}

	// Select runtime-specific content.
	contextComment := contextCommentOpencode
	reviewInstructions := reviewInstructionsOpencode
	compoundInstructions := compoundInstructionsOpencode
	if runtime == RuntimeClaude {
		contextComment = contextCommentClaude
		reviewInstructions = reviewInstructionsClaude
		compoundInstructions = compoundInstructionsClaude
	}

	rendered := string(data)
	rendered = strings.ReplaceAll(rendered, "{{context_comment}}", contextComment)
	rendered = strings.ReplaceAll(rendered, "{{review_instructions}}", reviewInstructions)
	rendered = strings.ReplaceAll(rendered, "{{compound_instructions}}", compoundInstructions)
	rendered = strings.ReplaceAll(rendered, "{{land_steps}}", landSteps)
	rendered = strings.ReplaceAll(rendered, "{{land_donts}}", landDonts)
	rendered = strings.ReplaceAll(rendered, "{{task_id}}", taskID)

	// Catch template typos (e.g., "{{ task_id }}" with spaces) that would
	// leave unresolved variables in the prompt.
	if strings.Contains(rendered, "{{") {
		source := "embedded"
		if promptDir != "" {
			source = filepath.Join(promptDir, filename)
		}
		return "", fmt.Errorf("unresolved template variable in %s", source)
	}

	return rendered, nil
}
