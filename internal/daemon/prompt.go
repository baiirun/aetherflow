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

	// Spawn-specific landing instructions. These differ from the daemon worker
	// versions because there's no prog task to mark — the branch/PR is the
	// only deliverable.
	spawnLandStepsNormal = `2. **Push your branch** -- from inside your worktree: ` + "`git push -u origin HEAD`" + `. If push fails (no remote configured), that's fine — the branch still exists locally.
3. **Create PR** -- if push succeeded, create a PR with a clear title and description summarizing the change. If push failed, skip this step.
4. **Clean up worktree** -- remove your worktree: ` + "`git worktree remove .aetherflow/worktrees/{{spawn_id}}`"

	spawnLandStepsSolo = `2. **Pull latest main** -- before merging, ensure your local main is up to date:
   ` + "```bash" + `
   git checkout main
   git pull origin main
   ` + "```" + `
   If pull fails (no remote), that's fine — continue with local state.
3. **Merge to main** -- from the project root (NOT the worktree):
   ` + "```bash" + `
   git merge af/{{spawn_id}} --no-ff -m "Merge af/{{spawn_id}}: <brief summary>"
   ` + "```" + `
   If the merge has conflicts, try to resolve them. If conflicts are too complex to resolve cleanly, abort the merge and stop.
4. **Push main** -- ` + "`git push origin main`" + `. If push fails (no remote), that's fine — the merge is local.
5. **Clean up** -- remove the branch and worktree:
   ` + "```bash" + `
   git worktree remove .aetherflow/worktrees/{{spawn_id}}
   git branch -d af/{{spawn_id}}
   ` + "```"

	spawnLandDontsNormal = `- Don't merge your PR -- just create it and let a human review.`

	spawnLandDontsSolo = `- Don't leave your branch unmerged -- in solo mode you are responsible for merging to main.
- Don't forget to delete the branch after merging -- clean up after yourself.`
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
//
// Returns the rendered prompt string ready to pass as the message argument
// to "opencode run".
func RenderPrompt(promptDir string, role Role, taskID string, solo bool) (string, error) {
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

	rendered := string(data)
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

// RenderSpawnPrompt reads the spawn prompt template and renders it with the
// user's freeform prompt and a generated spawn ID.
//
// This is the spawn-mode equivalent of RenderPrompt. Instead of a task ID
// looked up from prog, it takes a user-supplied prompt string that becomes
// the agent's objective. The spawn ID is used for worktree/branch naming.
//
// Recognized variables:
//   - {{user_prompt}} — the freeform user prompt (the agent's objective)
//   - {{spawn_id}} — unique identifier for worktree and branch naming
//   - {{land_steps}} — spawn-specific landing instructions (solo vs normal)
//   - {{land_donts}} — spawn-specific "what not to do" rules for landing
func RenderSpawnPrompt(promptDir string, userPrompt string, spawnID string, solo bool) (string, error) {
	// Reject user prompts containing template markers before substitution.
	// The replacement order is safe (user_prompt is last), but a prompt
	// containing "{{" would trigger the unresolved-variable check below
	// with a confusing error message. Catch it early with a clear message.
	if strings.Contains(userPrompt, "{{") {
		return "", fmt.Errorf("user prompt must not contain '{{' (conflicts with template syntax)")
	}

	const filename = "spawn.md"

	var data []byte
	var err error

	if promptDir == "" {
		data, err = fs.ReadFile(promptsFS, "prompts/"+filename)
		if err != nil {
			return "", fmt.Errorf("reading embedded prompt %s: %w", filename, err)
		}
	} else {
		path := filepath.Join(promptDir, filename)
		data, err = os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading prompt %s: %w", path, err)
		}
	}

	landSteps := spawnLandStepsNormal
	landDonts := spawnLandDontsNormal
	if solo {
		landSteps = spawnLandStepsSolo
		landDonts = spawnLandDontsSolo
	}

	rendered := string(data)
	rendered = strings.ReplaceAll(rendered, "{{land_steps}}", landSteps)
	rendered = strings.ReplaceAll(rendered, "{{land_donts}}", landDonts)
	rendered = strings.ReplaceAll(rendered, "{{spawn_id}}", spawnID)
	rendered = strings.ReplaceAll(rendered, "{{user_prompt}}", userPrompt)

	if strings.Contains(rendered, "{{") {
		source := "embedded"
		if promptDir != "" {
			source = filepath.Join(promptDir, filename)
		}
		return "", fmt.Errorf("unresolved template variable in %s", source)
	}

	return rendered, nil
}
