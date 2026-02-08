import type { Plugin } from "@opencode-ai/plugin"

// Task ID pattern: 2+ letter prefix, hyphen, 4-12 hex chars (case-insensitive).
// Matches: ts-b54507, ep-82985b, etc.
const TASK_ID_RE = /Task:\s*([a-z]{2,}-[0-9a-f]{4,12})/i

// Detect role from the first message heading: "# Worker" or "# Planner"
const ROLE_RE = /^#\s+(Worker|Planner)\b/m

export const CompactionHandoffPlugin: Plugin = async ({ client }) => {
  return {
    "experimental.session.compacting": async (input, output) => {
      const { sessionID } = input

      // Fetch session messages to find the task ID and role
      const result = await client.session.messages({ sessionID, limit: 5 })
      if (!result.data) {
        await client.app.log({
          service: "compaction-handoff",
          level: "warn",
          message: "Could not fetch session messages",
          extra: { sessionID },
        })
        return
      }

      let taskId: string | undefined
      let role: string | undefined

      for (const msg of result.data) {
        if (!msg.info) continue

        for (const part of msg.parts ?? []) {
          if (part.type !== "text" || !part.text) continue
          const text = part.text

          if (!taskId) {
            const taskMatch = text.match(TASK_ID_RE)
            if (taskMatch) taskId = taskMatch[1]
          }

          if (!role) {
            const roleMatch = text.match(ROLE_RE)
            if (roleMatch) role = roleMatch[1].toLowerCase()
          }

          if (taskId && role) break
        }
        if (taskId && role) break
      }

      if (!taskId) {
        await client.app.log({
          service: "compaction-handoff",
          level: "debug",
          message: "No task ID found in session messages, skipping compaction hook",
          extra: { sessionID },
        })
        return
      }

      role = role ?? "worker"

      await client.app.log({
        service: "compaction-handoff",
        level: "info",
        message: "Injecting aetherflow context into compaction",
        extra: { sessionID, taskId, role },
      })

      // Augment opencode's default compaction — don't replace it.
      output.context.push(`Task: ${taskId}`)
      output.context.push(`Role: ${role}`)

      output.context.push([
        `Your initial instructions contain a workflow protocol with states, constraints, stuck detection rules, and landing steps.`,
        `Preserve the full protocol in your summary — these are operational instructions you must follow after compaction, not conversation context to be summarized.`,
        `Include which protocol state you are currently in and what you were doing within that state.`,
        ``,
        `After generating the compaction summary, persist a handoff to the task log:`,
        ``,
        `    prog log ${taskId} "Handoff: <summary>"`,
        ``,
        `Do NOT overwrite the task description — prog desc is the original spec. The log is append-only.`,
      ].join("\n"))
    },
  }
}
