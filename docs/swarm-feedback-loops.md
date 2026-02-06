# Swarm Feedback Loops (Working Notes)

Notes from synthesizing Anthropic's C compiler project, Cursor's self-driving codebases, Amp's feedback-loopable design, and Claude Code's agent teams docs — mapped against aetherflow's existing architecture.

## The core loop

Everything reduces to:

```
pick task → do work → check work → yield or continue
```

An agent picks a task from prog, does the work, verifies it, and either marks it done or retries. When done, it picks the next task. The swarm stays alive because `prog ready` always has something to offer.

## Feedback is the product

Every source agreed: the feedback loop is the #1 investment. Not the scheduler, not the messaging — the mechanism by which an agent knows whether its work is correct.

For spec-driven work (compilers, browsers), the spec IS the feedback. There's an oracle — GCC, web standards — that provides ground truth. Most real work doesn't have this. "Done" is a judgment call.

For autonomous swarms where agents also plan and define done, the feedback comes from **the worker's own verify-review loop** — the worker implements, self-reviews using subagents with different perspectives, fixes, and iterates until clean.

## Two roles

### Planner

Takes an intent and the current feature matrix and produces the delta: new behaviors (matrix rows) and tasks with behavioral DoDs in prog. The planner is a PM — it owns outcomes, not implementations. It never prescribes HOW something should be built.

- Input: intent (epic, feature, problem statement) + current feature matrix
- Output: new matrix rows (expected behaviors) + tasks with behavioral DoDs in prog
- Primary tool: the feature matrix — the gap between current matrix and intent IS the task list
- Does: identify missing behaviors, write outcome-oriented DoDs, define dependencies, size tasks for a single agent session
- Does not: write code, prescribe implementations, decide technical approach
- May read: the codebase to verify the matrix is current (what exists today), not to design solutions
- Maps to: `/workflows:brainstorm` → `/workflows:plan`

### Worker

Claims a task, implements it, reviews its own work, fixes issues, and loops until clean. Review is part of the worker's loop, not a separate role — the worker invokes `/workflows:review` which spawns fresh subagents (security, performance, simplicity, etc.) that read the diff cold with no sunk cost in the implementation.

- Input: a claimed task with a DoD from prog
- Output: commits + test results + review-clean code
- Does: implement against the DoD, run tests, invoke review, fix findings, yield when clean
- Does not: create tasks (except: logging out-of-scope issues as new tasks), modify code outside the task's scope
- Maps to: `/workflows:work` → `/workflows:review` → fix → repeat

### The worker's inner loop

```
claim task from prog
implement
run tests (fast, inner loop)
run af:review (spawns subagent reviewers)
  P1 findings? → fix, re-review
  P2/P3 in scope? → fix, re-review
  P2/P3 out of scope? → log as new tasks in prog
  clean? → land the plane, yield
```

The review subagents provide the adversarial tension. They have different lenses and no sunk cost. This is effectively external review without a separate reviewer pool in the swarm.

### Why not a separate reviewer role

A dedicated reviewer pool creates a scheduling problem — you need to balance workers and reviewers, and reviews back up when there aren't enough reviewers. Since `/workflows:review` already spawns specialized subagents, every worker carries its own review capacity. Review scales automatically with implementation.

## Verification

### Two speeds

**Inner loop (while working):** fast, cheap, constant. Tests, build, lint — as part of the edit-compile-test cycle. Runs many times per task. This is Amp's "make it feedback loopable" insight.

**Exit check (when done):** thorough, once per task. Full test suite, lints, formatting, artifact verification, task state update, learnings capture. This is `/land-the-plane`.

### Swarm adaptation

Distinguish "I broke it" from "it was already broken."

When verification fails:
1. Failures in code you changed → fix and re-verify
2. Pre-existing failures (exist on main) → note in handoff, proceed
3. Not sure → compare against main branch to determine

Accept some error rate, trust the system to converge (Cursor's finding). Pre-existing failures get logged and picked up as new tasks.

### Output design

From Anthropic: design verification output for the agent, not for the human.

- Short. Don't dump 10k lines of test output. Summarize.
- Greppable. Every check ends with PASS or FAIL on one line.
- Full output goes to a file if the agent needs to dig deeper.

```
=== LINT === PASS (0 issues)
=== TEST === FAIL (2 failed, 47 passed)
  FAIL internal/inbox: TestPeekSinceFiltering
  FAIL internal/outbox: TestReplayAfterCrash
=== BUILD === PASS
```

## Task granularity

Cursor landed on 20-50 tool calls per task as the sweet spot. Too big and workers drift. Too small and coordination overhead dominates.

Planners need to produce tasks that are:
- Small enough for a single agent session
- Self-contained (clear inputs, clear DoD)
- Independently verifiable
- Parallelizable where possible (serialized where they'd collide)

## Constraints

Constraints are more effective than instructions (Cursor's key finding). The model already knows how to code. You don't teach it — you define boundaries.

### Role constraints

Each role has a clear boundary defined by what goes in and what comes out. The input/output contract IS the constraint.

The worker exception: "if you find something broken outside your scope, log it as a new task." Workers don't fix it — they create a task in prog. Information flows through the queue.

### System constraints

Universal rules regardless of role:

- No partial implementations. Ship complete work or don't ship.
- Run verification before yielding a task.
- Stay in your task's scope. Out-of-scope findings become new tasks.
- Update prog state before ending your session.
- Log what you tried that didn't work (not just what succeeded).

### Constraint hierarchy

When constraints conflict, more specific wins:

1. **System constraints** — always apply, never overridden
2. **Role constraints** — what you do and don't do based on your role
3. **Project constraints** — project-specific rules (from AGENTS.md, .aetherflow/config, etc.)
4. **Task constraints** — scope and DoD for this specific task

## Definition of done

The DoD is the contract between planner, worker, and the review subagents:

- **Planner writes it** — outcome-oriented, verifiable
- **Worker satisfies it** — runs the checks, meets the criteria
- **Review subagents audit it** — was the DoD good enough? did the work actually solve the problem?

### What a good DoD looks like

Describe the outcome, not the steps. Include something the agent can actually test or measure. If you can't describe a verifiable outcome, the task needs to be broken down further.

Good — concrete outcomes with verification:
> Users can filter by date range. The query uses the existing index, not a table scan. The endpoint returns results in under 200ms for 10k rows. Run: `go test ./internal/api/... -v`

Good — behavioral description with edge cases:
> The inbox poll returns only messages newer than the --since timestamp. Exact-timestamp matches are excluded (strictly greater than). Empty inboxes return an empty list, not an error. Run: `go test ./internal/inbox/... -v -count=1`

Bad — vague, uncheckable:
> Make the filtering work well.

Bad — checkbox mentality (implementation steps, not outcomes):
> - [ ] Add a filter dropdown
> - [ ] Write a SQL query
> - [ ] Add an index
> - [ ] Write tests

The checkbox version makes agents focus on satisfying the list rather than thinking about the problem (Cursor's finding).

### Staying in scope

- Don't fix things outside your task scope
- If you find issues outside your scope, log them as new tasks in prog
- Don't go down rabbit holes fixing pre-existing problems
- Review findings outside the task's scope become new tasks, not rework
- The swarm converges because agents propagate context about what's broken, not because every commit is clean

## Context freshness

Agents drift over long sessions. The mitigation is keeping the loop tight — each task is one unit of work, claim-do-verify-yield.

### Compaction and handoff are the same thing

OpenCode's compaction prompt is simple and works well:

```
Focus on information that would be helpful for continuing the conversation, including:
- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- Key user requests, constraints, or preferences that should persist
- Important technical decisions and why they were made
```

This is a handoff prompt. It doesn't matter whether you're handing off to yourself (after context compaction) or to another agent (task reassignment) or to no one yet (session end). The information needed is the same.

One format. Used at context pressure, step transitions, session end, and blocked/escalation.

Don't over-structure it. The freeform prompt lets the model decide what's important. It has the full context. It knows.

The only structured data worth persisting alongside the freeform handoff: **task_id** (what), **agent_id** (who). Everything else is already tracked by prog.

Handoff prompt for aetherflow (adds "what didn't work" — every post agreed this is the most valuable handoff information):

```
Summarize for the next agent picking up this work.
Focus on what would be helpful for continuing, including:
- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- What was tried and didn't work, and why
- Key constraints or decisions that should persist
- Important technical decisions and why they were made
```

### prog as the persistence layer

The handoff goes into prog, not into the conversation. Conversations are ephemeral — they get compacted, sessions end, agents restart. Prog persists.

- `prog desc <id> "rewritten summary"` — replace the task description with current truth. This is what the next agent reads first.
- `prog log <id> "what happened"` — append-only audit trail. For history, not for working context.
- `prog append <id> "additional context"` — add to a description that's still correct but incomplete.

The description is always the current truth. When the situation changes, rewrite it. The old state lives in `prog log`.

## Architecture

Two layers.

### Prog — task state (persistent)

The source of truth for all task state. Survives agent crashes, daemon restarts, everything. Tasks, assignments, DoDs, handoffs, logs, priorities.

### Daemon — process supervisor with simple scheduling

A process that maintains a pool of N agent slots. Its logic is:

```
loop:
  tasks = prog ready
  running = count active agents
  available = pool_size - running

  for each available slot:
    task = tasks.pop()           # highest priority first
    role = has_plan(task) ? worker : planner
    spawn(role, task)

  sleep(interval)
```

Role inference is a few lines of code, not an LLM call. The task's state in prog tells you what it needs. Anthropic ran 16 agents and 2000 sessions with a bash while loop. Cursor's spawning was deterministic too. None of the blog posts used an LLM for scheduling.

The daemon also:
- Watches for crashed/timed-out agents and clears their slots
- Writes agent activity to an observable state file
- Respawns on the same task after a crash (with handoff context from prog)

### Multi-agent tasks

Most tasks get one agent. Some tasks — debugging with competing hypotheses, research from multiple angles — benefit from multiple agents working the same problem.

The daemon spawns all agents at once on the same task. Each gets an agent ID so they can message each other via aetherflow's inbox/outbox. The daemon decides how many based on a hint in the task (e.g. "investigate: 3 agents") or defaults to 1.

### Crash recovery

1. Daemon detects agent exit (process died, timeout exceeded)
2. Task remains "in progress" in prog with whatever was last logged
3. Daemon spawns a new agent on the same task
4. New agent reads the handoff from `prog desc` and continues
5. If no handoff was written (early crash), agent starts fresh from the original description and DoD

## Observability

The opencode plugin hooks `tool.execute.after` and writes each tool call to a log file keyed by agent ID. The daemon reads these. Everything is inspectable.

### af status — full swarm view

```
$ af status

Pool: 3/5 slots active

  worker-1   ts-abc  38m  implementing  "Implement inbox polling"
  worker-2   ts-def   3m  verifying     "Add rate limiting"
  worker-3   ts-ghi  12m  reviewing     "Add user preferences"
  —          —        —   idle
  —          —        —   idle

Queue: 4 pending, 1 blocked
  ts-jkl  pending   P1  "Fix auth token expiry"
  ts-mno  pending   P2  "Add user preferences endpoint"
  ts-pqr  pending   P2  "Refactor config loading"
  ts-stu  pending   P3  "Update API docs"

Recent:
  worker-1  ts-xyz  done     14m ago  "Refactor database pool"
  worker-4  ts-uvw  crashed   6m ago  "Parse config file"
```

### af status <agent> — agent detail

```
$ af status worker-1

Task: ts-abc "Implement inbox polling"
Running: 38m
Phase: implementing

Last activity: 2m ago
  tool: edit /internal/inbox/inbox.go

Recent:
  36m  read task from prog
  34m  read /internal/inbox/inbox.go
  31m  edit /internal/inbox/inbox.go
  28m  bash: go test ./internal/inbox/...  → FAIL (2)
  25m  edit /internal/inbox/inbox.go
  22m  bash: go test ./internal/inbox/...  → FAIL (1)
  18m  edit /internal/inbox/inbox.go
  14m  bash: go test ./internal/inbox/...  → PASS
  10m  edit /internal/inbox/inbox_test.go
   5m  bash: go test ./internal/inbox/...  → FAIL (1)
   2m  edit /internal/inbox/inbox.go

Verification: 2 pass, 3 fail attempts
Context: 62% full
```

Tests going from 2 failures to 1 = progress, leave it alone. Same failure repeated 6 times with the same edit = stuck, kill it.

### Stuck detection

Signals:
- No new tool calls for N minutes (heartbeat stopped)
- Same test failure repeated 3+ times with similar edits (retry loop)
- Context at 90%+ with no compaction (about to lose context)

The plugin writes a heartbeat. The daemon watches it.

### Intervention commands

- `af status` — full swarm view
- `af status <agent>` — agent detail with tool call history
- `af kill <agent>` — kill a stuck agent, free the slot
- `af drain` — stop spawning, let current work finish
- `af pause` — freeze the pool
- `af logs <agent>` — tail the agent's session

## How agents get their prompts

### MVP: daemon renders, `opencode run` delivers

The daemon reads the role prompt template (`prompts/worker.md` or `prompts/planner.md`),
replaces `{{task_id}}` with the actual task ID, and passes the rendered prompt as
the first message to `opencode run "<rendered prompt>"`.

No plugin. No env vars. No `system.transform` hook.

The agent self-serves everything else (project name, description, DoD, learnings)
via `prog show <task_id>` during its orient step. See `prompts/README.md` for the
assembly flow diagram.

Implementation details: `docs/plans/2026-02-06-feat-prompt-rendering-and-agent-spawn-plan.md`

### Future: opencode plugin for advanced hooks

When we need capabilities beyond what `opencode run` provides, we'll add a plugin:

| Hook | Purpose |
|------|---------|
| `experimental.session.compacting` | Replace compaction prompt with aetherflow's handoff prompt |
| `tool.execute.after` | Write tool calls to agent activity log (observability) |
| `session.idle` | Write handoff to prog, mark task done/blocked |
| `event` | Heartbeat, status updates |
| `tool` (custom) | `claim_task`, `yield_task`, `send_message` tools for the agent |

These are tracked in epic `ep-ca386e` and are not needed for MVP.

## System prompt design

### How prompts are assembled

The agent's prompt has three layers:

```
[opencode's default system prompt — already present, not ours to modify]
[aetherflow role prompt — from opencode run, the rendered template]
[agent-fetched context — prog show, prog context, etc. during orient]
```

Layers 1 and 2 are present before the first tool call. Layer 3 is fetched
by the agent as its first action.

The role prompt defines the protocol and constraints. The agent fetches task
specifics at runtime — this keeps prompts static and avoids the daemon needing
to call prog for every spawn.

### Three sections per prompt

Each role prompt has three sections:

1. **ROLE** — identity, input/output contract, hard constraints (what you NEVER do)
2. **PROTOCOL** — the state machine, decision points, what triggers transitions
3. **EXIT** — when you're done, what to persist, how to hand off

### Design principles applied

- **Constraints over instructions.** (Cursor) The model knows how to code. Define boundaries, not procedures. "No partial implementations" > "remember to finish."
- **Prescriptive state transitions, not prescriptive steps.** The states are explicit (implement → verify → review → fix → land). Within each state, the agent decides what to do. The `/work` and `/review` skills provide detailed guidance for each state.
- **Specificity for verification.** (Cursor) Vague verification = vague work. DoDs include concrete commands. The prompt tells the agent to establish its feedback loop BEFORE implementing.
- **Self-detection of stuck.** (Anthropic) Agents are time-blind. The prompt includes: "3 similar attempts with the same failure = stop, log, yield."
- **Feature matrix is explicit.** Both roles interact with it. Planners add rows. Workers update coverage. This is in the prompt, not hidden in the plugin.
- **What didn't work is the most valuable handoff.** (All posts) Emphasized in the exit section.
- **Documentation compounds understanding.** Every task should leave the codebase more understandable than it found it. Code comments explain WHY, not what. Descriptions capture design decisions and tradeoffs. ASCII diagrams make structure visible. Tests read as behavioral specifications. This is how a swarm builds institutional knowledge — not through a knowledge base, but through the codebase itself.

### Planner prompt design

**ROLE**

You are a planner. You take an intent and produce tasks with behavioral definitions of done.

Input: an intent (epic, feature, problem statement) and the current feature matrix.
Output: new feature matrix rows + tasks with behavioral DoDs in prog.

You own outcomes, not implementations. You define WHAT the system should do, never HOW it should be built. Workers decide implementation.

Hard constraints:
- Never write code. Not even "just a small helper."
- Never prescribe implementation approach. No "use a hash map" or "add a column." Describe the behavior.
- DoDs describe outcomes, not steps. No checkbox lists.
- Every DoD includes a verification command or describes a verifiable condition.
- If you can't describe a verifiable outcome, the task needs to be broken down further.

**PROTOCOL**

States: `orient → identify gaps → write tasks → verify completeness`

`orient`: Read the intent. Read the current feature matrix. Understand what the system does today. If the matrix might be stale, explore the codebase to verify it reflects reality — but only to understand what EXISTS, not to design solutions.

`identify gaps`: The delta between the intent and the current matrix is your task list. Each missing behavior is a candidate task. Group related behaviors into tasks sized for a single agent session (20-50 tool calls). Note: you're identifying missing outcomes, not missing code.

`write tasks`: For each task:
- Write a behavioral DoD (what the system should do when this task is done, including edge cases)
- Include a verification command or describe how the worker can confirm success
- Specify dependencies (which tasks must complete first)
- Add the expected behaviors as new rows in the feature matrix (coverage = "not covered" — the worker will update this)

`verify completeness`: Review your task list against the intent. Is anything missing? Are tasks self-contained — could a worker pick up any single task with no knowledge of the others? Are DoDs verifiable? Would the feature matrix, once all rows are covered, fully describe the intent?

**EXIT**

Planner DoD (your own definition of done):
- All tasks exist in prog with behavioral DoDs
- All expected behaviors are rows in the feature matrix
- Dependencies are specified
- A worker could pick up any task and start without asking questions
- The feature matrix, if all new rows were covered, would fully describe the intent

Hand off via the standard handoff prompt. Persist to prog.

### Worker prompt design

**ROLE**

You are a worker. You claim a task, implement it, verify it, review it, and yield when clean.

Input: a claimed task with a DoD from prog.
Output: committed code, passing tests, clean review, updated feature matrix coverage.

Hard constraints:
- Stay in scope. Your task, your DoD, your files. Nothing else.
- No partial implementations. Ship complete work or don't ship.
- Out-of-scope issues → `prog add` a new task. Do not fix them.
- Distinguish "I broke it" from "it was already broken." Compare against main if unsure.
- 3 similar attempts with the same failure → stop. Log what you tried and why it failed. Yield the task.
- Log what you tried that didn't work. This is the most valuable handoff information.

**PROTOCOL**

States: `orient → build feedback loop → implement → verify → review → fix → land`

`orient`: Read the task and DoD. Understand what "done" looks like BEFORE writing any code. Read relevant learnings. Read any handoff notes from a previous agent.

`build feedback loop`: Before implementing, establish how you'll verify your work. If the DoD includes a verification command, confirm it works (run it, see the current state). If not, create one — a test, a curl command, a script. You need a fast way to check your work repeatedly during implementation. This is the most important step. (Amp: agents are most powerful when they can validate their work against reality.)

`implement`: Write the code. Run your feedback loop frequently — after every meaningful change, not just at the end. Fast inner loop: edit → verify → adjust.

`verify`: Run full verification. The DoD's verification command + full test suite + lint + build. Use the standard output format:
```
=== LINT === PASS/FAIL
=== TEST === PASS/FAIL (N failed, M passed)
=== BUILD === PASS/FAIL
```
If failures are in code you changed → go to `fix`. If pre-existing (exist on main) → note in handoff, continue.

`review`: Invoke `/workflows:review`. This spawns fresh subagent reviewers with different lenses. They read your diff cold.

`fix`: Review findings come back prioritized.
- P1 (bugs, correctness) → fix, return to `verify`
- P2/P3 in scope → fix, return to `verify`
- P2/P3 out of scope → `prog add` a new task, continue
- No findings → go to `land`

`land`: Final exit check.
1. Run full verification one more time (DoD verification + full test suite + lint + build)
2. Update feature matrix: for each behavior in your DoD, set coverage status and verification command
3. Log learnings: did you discover anything that would help future agents? `prog learn`
4. Write handoff to prog
5. Mark task done: `prog done`

**EXIT**

Worker DoD (your own definition of done):
- All DoD criteria from the task are satisfied
- Full test suite passes
- Review is clean — no P1s, no in-scope P2/P3s remaining
- Feature matrix coverage is updated for your behaviors
- Learnings logged if any
- Handoff written to prog
- Task marked done in prog

### Handoff prompt (shared, both roles)

Used at: context compaction, session end, blocked/escalation, task yield.

```
Summarize for the next agent picking up this work.
Focus on what would be helpful for continuing, including:
- What was done
- What is currently being worked on
- Which files are being modified
- What needs to be done next
- What was tried and didn't work, and why
- Key constraints or decisions that should persist
- Important technical decisions and why they were made
```

## Compounding — learning capture and knowledge reuse

Research from CASS (Collaborative Agent Swarm System), AgentFS, Cursor's dynamic context discovery, and the arxiv paper "Everything is Context."

### The problem

Agents repeat mistakes. Agent A discovers that a particular API is flaky and needs retries — then crashes. Agent B picks up the task and hits the same flaky API and wastes 20 minutes rediscovering the same thing. This compounds across a swarm.

The flip side: agents also discover useful patterns, shortcuts, and caveats. These currently live in handoff notes and prog logs, but they're buried — the next agent has to read through everything to find the relevant insight.

### Key findings from research

**Dynamic discovery, not static injection** (Cursor, CASS). Don't inject all learnings into every agent's system prompt. It bloats context and most learnings aren't relevant to most tasks. Instead:

- Learnings are stored in prog as tagged, searchable entries
- Agents query for relevant learnings at task start: `prog context <task-id>` returns learnings matching the task's tags, affected files, or domain
- The agent decides what's relevant from the results, not the system prompt

This is how Cursor does it — dynamic context discovery at the point of need, not a static knowledge dump.

**Confidence decay** (CASS, arxiv paper). Learnings aren't equally valuable forever. A workaround for a bug in a dependency becomes obsolete when the dependency is updated. A pattern that worked for 3 agents might fail for the 4th in a different context.

- Each learning has a confidence score (starts at 1.0)
- Confidence decays over time (configurable half-life, e.g. 30 days)
- Positive/negative feedback adjusts confidence: `prog mark <id> --helpful` / `prog mark <id> --harmful`
- Low-confidence learnings are deprioritized in search results
- Zero-confidence learnings are archived (not deleted — the history matters)

**Feedback loop on learnings** (AgentFS). Agents should mark whether a learning was actually useful. This closes the loop — learnings that keep helping get reinforced, learnings that mislead get demoted.

When an agent uses a learning during a task:
1. Agent logs that it used learning L-xyz
2. At task completion, agent marks each used learning as helpful or harmful
3. Confidence adjusts accordingly

This is cheap. A few `prog mark` calls at the end of a task. The value compounds — after 50 tasks, the system knows which learnings are gold and which are noise.

**Periodic synthesis** (CASS, AgentFS). Raw learnings accumulate. Duplicates pile up. Contradictions appear. A synthesis task runs periodically (or when learning count exceeds a threshold) and:

- Merges duplicate learnings (same insight, different wording)
- Resolves contradictions (if two learnings conflict, the one with higher confidence wins, the other gets a caveat)
- Inverts failed rules into anti-patterns ("don't do X because Y")
- Promotes high-confidence learnings into project-level documentation
- Archives low-confidence, old learnings

This is a planner-type task — the daemon can schedule it as a recurring task in prog. The synthesis agent reads all learnings, writes a merged/cleaned set, and logs what was changed.

### Learning types

Not all learnings are equal. Categorizing helps search:

| Type | Example | Persistence |
|------|---------|-------------|
| **Gotcha** | "The payments API returns 200 even on failure — check the response body" | Long-lived until the API changes |
| **Pattern** | "All service tests need a test database — use `testdb.Setup(t)`" | Long-lived, project-level |
| **Workaround** | "Module X has a race condition — add a 100ms delay before polling" | Short-lived, decays when the bug is fixed |
| **Decision** | "We chose Postgres over SQLite because of concurrent write support" | Permanent — decisions don't decay |
| **Anti-pattern** | "Don't use global state for request-scoped config — causes cross-contamination" | Long-lived |

### How it works in practice

1. **Capture**: At session end (`session.idle` hook or `/land-the-plane`), the agent is prompted: "Did you learn anything that would help future agents working in this area?" Freeform. Gets stored via `prog learn "insight" --tags "payments,api,retry"`.

2. **Discovery**: At task start, the plugin calls `prog context <task-id>` and injects relevant learnings into the system prompt (limited — top 5 by relevance × confidence, not all of them).

3. **Feedback**: At task end, the agent marks learnings it used as helpful or harmful.

4. **Synthesis**: A recurring task in prog triggers a synthesis agent that cleans up, merges, and promotes learnings.

### What prog already supports

- `prog learn "insight" --tags "x,y,z"` — store a learning (exists)
- `prog context <task-id>` — return learnings relevant to this task (exists)

Confidence decay, learning feedback (`prog mark`), and periodic synthesis are future work. Start simple — capture learnings, retrieve them — and add curation when we see the need.

## Regression detection — the feature matrix

### The problem

Agent A implements feature A, tests pass, ships. Agent B implements feature B, subtly breaks feature A. B's tests pass (they test B, not A). B's review doesn't catch it (the breakage is subtle). Feature A is now broken. Nobody knows.

The current design catches regressions only within the scope of the agent that made the change. There's no mechanism that says "verify all previously completed work still works." That doesn't scale. But without it, the swarm silently degrades.

The root cause: **verification is only as good as test coverage, and test coverage is always incomplete.** The system needs to know what's covered and what isn't — explicitly, not by hoping tests exist.

### The feature matrix as the oracle

Anthropic had GCC as ground truth. Most projects don't have an oracle. The feature matrix IS the oracle — an explicit, living map of "what the system should do" that exists independently of the code and tests.

The matrix is a structured document (or a prog data structure) with one row per behavior:

| Feature | Behavior | Verification | Coverage | Last verified |
|---------|----------|-------------|----------|---------------|
| Auth | Login with valid credentials returns 200 + token | `go test ./internal/auth/... -run TestLoginSuccess` | covered | 2025-02-04 |
| Auth | Login with invalid password returns 401 | `go test ./internal/auth/... -run TestLoginBadPassword` | covered | 2025-02-04 |
| Auth | Expired token returns 401, not 500 | — | **not covered** | never |
| Inbox | Poll returns only messages after --since | `go test ./internal/inbox/... -run TestPeekSince` | covered | 2025-02-03 |
| Inbox | Empty inbox returns empty list, not error | — | **not covered** | never |

The "not covered" rows are the known gaps. When a regression hits an uncovered area, the post-mortem is: "this gap was visible. We chose not to cover it yet." That's a different conversation than "we had no idea."

### Who maintains it

**Planners add rows.** When a planner creates tasks for a new feature, it also specifies the expected behaviors. This is part of the planner's output — you haven't finished planning until the behaviors are defined. These become the DoD for the worker AND the rows in the matrix.

**Workers fill in coverage.** Part of the worker's exit check: "for each behavior I implemented, which test covers it? Update the matrix with the test reference and coverage status." If a behavior has no test, the worker either writes one or logs a task to add coverage.

**Synthesis updates it.** The periodic synthesis task (from the compounding section) also audits the matrix — checking for stale rows, behaviors that no longer match the code, and coverage gaps that have persisted too long.

### Three layers of regression defense

**Layer 1: Full test suite at exit (per-task, automatic)**

Every worker runs the full test suite as part of the exit check (`land` state). This is the baseline — it catches regressions that have test coverage, regardless of which files were touched.

**Layer 2: Full matrix validation (periodic, scheduled)**

The daemon schedules a recurring task: "validate the feature matrix." The agent:

1. Runs every verification command in the matrix
2. Reports which pass, which fail, which are missing
3. For failures: creates a regression task with `git bisect` guidance in the DoD ("this test passed as of commit X, find what broke it")
4. For missing coverage: creates a coverage task ("write a test for: expired token returns 401")
5. Updates the "last verified" column

This is essentially CI owned by the swarm. The difference from normal CI: the matrix makes the coverage model explicit. Normal CI runs whatever tests exist. Matrix validation runs tests AND identifies what's NOT tested.

How often? After every N completed tasks, or on a timer. Start with "every 10 completed tasks" and tune.

**Layer 3: Coverage gap prioritization (strategic, planner-driven)**

Not all uncovered behaviors are equally risky. The planner can prioritize coverage tasks by:

- **Blast radius**: behaviors in shared/core code are higher risk than isolated features
- **Change frequency**: code that changes often is more likely to regress
- **Severity**: "auth works" matters more than "tooltip is aligned"

The periodic synthesis task can surface: "these 5 behaviors have been uncovered for 2 weeks and are in high-change areas. Recommend writing tests."

### What this doesn't catch

- **Emergent behavior** that was never specified in the matrix (the system does something useful that nobody documented, then it breaks)
- **Performance regressions** (feature still works, but slower — needs different verification: benchmarks in the matrix)
- **Cross-service regressions** in distributed systems (feature A works in isolation, but breaks when service B changes its response format)

For emergent behavior: when a regression is discovered that wasn't in the matrix, the fix task also adds the row. The matrix grows from failures. This is the feedback loop.

For performance: add benchmark rows to the matrix with thresholds. "API response < 200ms for 10k rows." The periodic validation runs them.

### Where the matrix lives

TBD. Could be a project-level file (e.g. `MATRIX.md`) that agents read/write directly, or a prog data structure, or both. The format matters less than the discipline of maintaining it. Start with a markdown file and see if it needs to be structured data.

### Relationship to DoD

The matrix and DoD are two views of the same thing. The planner writes behaviors → those become the DoD for the worker → the worker implements and verifies → the matrix records what's covered. The matrix is the DoD's persistence layer. Individual task DoDs are ephemeral (they live in the task). The matrix is cumulative (it survives across tasks).

### Why not deterministic simulation testing

Deterministic simulation testing (FoundationDB, TigerBeetle, Antithesis) is the gold standard for finding bugs in complex systems. It works by:

1. **Controlling all nondeterminism.** Time, network, disk, randomness — everything goes through a swappable abstraction layer. The simulation replaces real I/O with controlled versions where the simulation decides when messages arrive, when disks fail, and how threads interleave.

2. **Checking invariants, not test cases.** The simulation doesn't know what a bug looks like. It knows what **correct** looks like, because you define invariants — properties that must always be true ("total money in the system never changes," "no two nodes are leader for the same term," "after a committed write, all reads return that value or newer"). The simulation runs millions of executions with different seeds and fault injections, checking invariants after every step.

3. **Reproducing via seeds.** Every execution is controlled by a seed. Same seed = same execution = same result. No flaky tests. When an invariant violation is found on seed 847291, that seed becomes a permanent regression test.

The simulation doesn't "find bugs" — it finds **invariant violations**. The invariants are the oracle. If you can't define them precisely enough to assert in code, the simulation can't help you.

**Why it doesn't fit our problem:**

- **Requires total I/O abstraction.** FoundationDB and TigerBeetle were designed around this from day one. You can't bolt it on. Our agents write arbitrary code in arbitrary projects.
- **Requires formal invariants.** Database correctness is well-defined (linearizability, durability). "The web app works correctly" is not. What's the invariant for "the email is sent at the right time"?
- **Enormous investment.** FoundationDB spent years on their simulation framework. It's an architectural commitment, not a testing strategy.
- **Our agents aren't deterministic.** LLM-driven workflows have inherent nondeterminism that can't be seeded away.

**What we borrow from it:**

The feature matrix borrows the three most valuable ideas:

| Simulation concept | Feature matrix equivalent |
|---|---|
| **Explicit invariants** — you define what correct means, the system checks it | **Explicit behaviors** — the matrix defines what the system should do, validation checks it |
| **Known interesting seeds** — when a failure is found, the seed that triggered it becomes a permanent test | **Growing from failures** — when a regression is found outside the matrix, the fix task adds the row. The matrix accumulates the project's scar tissue |
| **Coverage model is visible** — you know which invariants are checked and which state space is explored | **Coverage gaps are visible** — "not covered" rows show exactly where you're blind |

The matrix is a poor man's invariant checker. It's not exhaustive (only checks what's in the matrix), not deterministic (tests can be flaky), and doesn't explore the full state space (no fault injection). But it makes the coverage model explicit, and that's the insight worth stealing.

## Open questions

- What's the right pool size to start with? Probably small (3-5) and tune from observation.
- How do planners know the right task granularity? Is this learned from experience or defined upfront?
- How do we handle tasks where the DoD genuinely can't be concrete upfront (exploratory work, research, design)?
- What signals indicate a task needs multiple agents vs one?
- How does "DoD was insufficient" feedback from review subagents flow back? New task in prog to refine the plan?
- Should the matrix track negative behaviors too? ("System does NOT send email on draft save" — preventing regressions where something starts happening that shouldn't)

## Sources

- [Building a C Compiler with Claude](https://www.anthropic.com/engineering/building-c-compiler) — Anthropic
- [Self-Driving Codebases](https://cursor.com/blog/self-driving-codebases) — Cursor
- [Feedback Loopable](https://ampcode.com/notes/feedback-loopable) — Amp
- [Agent Teams](https://code.claude.com/docs/en/agent-teams) — Claude Code
- [OpenCode compaction](https://github.com/anomalyco/opencode/blob/8bf97ef/packages/opencode/src/agent/prompt/compaction.txt) — anomalyco/opencode
- [CASS: Collaborative Agent Swarm System](https://arxiv.org/abs/2501.05453) — arxiv
- [AgentFS](https://github.com/torantulino/agentfs) — Toran Bruce Richards
- [Everything is Context](https://arxiv.org/abs/2504.13039) — arxiv
- Cursor's dynamic context discovery — referenced in Self-Driving Codebases post
- [FoundationDB: Testing Distributed Systems](https://www.youtube.com/watch?v=4fFDFbi3toc) — Will Wilson, FoundationDB
- [TigerBeetle: Deterministic Simulation Testing](https://tigerbeetle.com/blog/2023-07-11-we-put-a-distributed-database-in-the-browser/) — TigerBeetle
- [Antithesis](https://antithesis.com/) — deterministic simulation testing as a service
