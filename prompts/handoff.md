# Handoff

Used at: context compaction, session end, blocked/escalation, task yield, task completion.

Write a summary for the next agent picking up this work.

Persist using both commands:
1. `prog desc <task-id> "<full summary>"` — update the task description with current truth (this is what the next agent reads first)
2. `prog log <task-id> "Handoff: <one-line summary>"` — append to the audit trail for history

Focus on what would be helpful for continuing, including:

- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- What was tried and didn't work, and why
- Key constraints or decisions that should persist
- Important technical decisions and why they were made
