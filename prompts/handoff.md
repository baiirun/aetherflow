# Handoff

Used at: context compaction, session end, blocked/escalation, task yield, task completion.

Write a summary for the next agent picking up this work.

Persist the handoff to the task log — do NOT overwrite the task description:

    prog log <task-id> "Handoff: <full summary>"

The task description (`prog desc`) is the original specification. Overwriting it destroys context that future agents need. The log is append-only and the next agent reads it via `prog show`.

Focus on what would be helpful for continuing, including:

- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- What was tried and didn't work, and why
- Key constraints or decisions that should persist
- Important technical decisions and why they were made
