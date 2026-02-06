import type { Plugin } from "@opencode-ai/plugin"
import { readFileSync } from "fs"
import { join } from "path"

const PREFIX = "[compaction-handoff]"

// Matches task IDs rendered by the Go daemon's RenderPrompt into worker.md/planner.md.
// Format: "Task: <prefix>-<hex>" where prefix is 2+ lowercase letters (ts-, ep-, etc.)
// and hex is 4-12 chars. This is a cross-boundary contract with prompts/worker.md line 8
// and prompts/planner.md line 8 — do not change the format without updating both sides.
const TASK_ID_PATTERN = /Task:\s*([a-z]{2,}-[a-fA-F0-9]{4,12})/

/**
 * Compaction handoff hook for aetherflow.
 *
 * When opencode compacts a session (context window full), this plugin:
 * 1. Replaces the default compaction prompt with aetherflow's handoff format
 * 2. Injects task context (task ID, role, key commands, compressed protocol)
 *    so they survive compaction
 * 3. Instructs the agent to persist the summary to prog for external visibility
 *
 * If no task ID is found in the conversation (not an aetherflow session),
 * the hook does nothing.
 */
export const CompactionHandoffPlugin: Plugin = async ({ client, directory }) => {
  const handoffPath = join(directory, "prompts", "handoff.md")

  return {
    "experimental.session.compacting": async (input, output) => {
      // Read handoff.md on each compaction so edits take effect without restart.
      // This aligns with the Go daemon's hot-reload philosophy (pool.go respawn).
      let handoffPrompt: string | undefined
      try {
        handoffPrompt = readFileSync(handoffPath, "utf-8")
      } catch {
        console.warn(`${PREFIX} ${handoffPath} not found — skipping prompt replacement`)
        // Still inject structured context below if we can find a task ID,
        // since the context block is more critical than the prose prompt.
      }

      // The compaction hook input only provides { sessionID }, not messages.
      // Use the SDK client to fetch conversation messages for task ID extraction.
      let messages: Array<{ info: { sender: string }; parts: Array<{ type: string; content?: string; text?: string }> }>
      try {
        messages = await client.session.messages({ path: input.sessionID })
      } catch (err) {
        console.warn(`${PREFIX} failed to fetch messages for session ${input.sessionID}:`, err)
        return
      }

      const { taskId, role } = extractTaskContext(messages)
      if (!taskId) {
        console.debug(`${PREFIX} no task ID found in session ${input.sessionID} — not an aetherflow session, skipping`)
        return
      }

      console.info(`${PREFIX} rewriting compaction for task ${taskId} (role: ${role})`)

      // Replace the compaction prompt with the handoff format.
      // The prompt instructs the agent to use `prog desc` (current truth)
      // plus `prog log` (audit trail) — matching the design in swarm-feedback-loops.md.
      if (handoffPrompt) {
        output.prompt = `${handoffPrompt}

Task ID: ${taskId}

After generating the summary, persist it so the daemon and future agents can see your progress:

1. Update the task description with current truth:
   prog desc ${taskId} "<your full handoff summary>"

2. Also append to the audit log:
   prog log ${taskId} "Compaction: <one-line summary of current state>"`
      }

      // Inject structured context that survives compaction.
      // This is separate from the prompt — it provides identity, commands,
      // and a compressed protocol so the agent knows HOW to work, not just
      // WHAT it's working on.
      output.context ??= []
      output.context.push(
        `## Aetherflow Task Context
- Task ID: ${taskId}
- Role: ${role}
- This session is managed by the aetherflow daemon

## Key Commands
- Read your task: \`prog show ${taskId}\`
- Update task description: \`prog desc ${taskId} "text"\`
- Log progress: \`prog log ${taskId} "message"\`
- Complete task: \`prog done ${taskId}\`
- Yield when stuck: \`prog block ${taskId} "reason"\`
- Create out-of-scope task: \`prog add "title" -p <project>\`
- Check learnings: \`prog context -p <project> --summary\`
- Find project name: shown in \`prog show ${taskId}\` output

## Protocol (compressed)
States: orient → feedback loop → implement → verify → review → fix → land
- Orient: read task, check for continuation (branch + logs), fill knowledge gaps
- Feedback loop: establish verification BEFORE coding (DO NOT SKIP)
- Implement: edit → verify → adjust. Checkpoint aggressively (commit + prog log)
- Verify: DoD command + tests + lint + build
- Stuck (3 same failures or 5 fix cycles): prog log what failed, prog block, stop
- Out-of-scope issues: prog add a new task, don't fix them
- No partial implementations. Ship complete or don't ship.
- Land: final verify, create PR, prog done`,
      )
    },
  }
}

/**
 * Extract task ID and role from conversation messages.
 *
 * The first user message contains the rendered worker/planner prompt.
 * - Task ID: parsed from "Task: <id>" in the prompt's Context block
 * - Role: inferred from the heading ("# Worker" or "# Planner")
 *
 * Returns { taskId, role } where role defaults to "worker" if undetectable.
 */
function extractTaskContext(
  messages: Array<{ info: { sender: string }; parts: Array<{ type: string; content?: string; text?: string }> }>,
): { taskId: string | null; role: string } {
  let role = "worker"

  for (const msg of messages) {
    if (msg.info?.sender !== "user") continue

    // Concatenate all text parts of the message
    const text = (msg.parts ?? [])
      .map((p) => p.content ?? p.text ?? "")
      .join("")

    // Detect role from prompt heading
    if (text.includes("# Planner")) {
      role = "planner"
    }

    const match = text.match(TASK_ID_PATTERN)
    if (match) return { taskId: match[1], role }
  }

  return { taskId: null, role }
}
