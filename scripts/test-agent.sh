#!/usr/bin/env bash
# test-agent.sh — Simulate what the daemon does: render a prompt and spawn an opencode session.
#
# Usage:
#   ./scripts/test-agent.sh worker ts-b30b98
#   ./scripts/test-agent.sh planner ep-01e787
#
# Output:
#   Streams opencode JSON events to stdout AND saves to .aetherflow/test-runs/<role>-<task_id>.jsonl
#
set -euo pipefail

ROLE="${1:?Usage: $0 <worker|planner> <task_id>}"
TASK_ID="${2:?Usage: $0 <worker|planner> <task_id>}"
PROMPT_DIR="${PROMPT_DIR:-prompts}"
TEMPLATE="$PROMPT_DIR/$ROLE.md"

if [[ ! -f "$TEMPLATE" ]]; then
  echo "ERROR: Template not found: $TEMPLATE" >&2
  exit 1
fi

# Render: replace {{task_id}} with actual task ID (same as daemon's RenderPrompt)
PROMPT=$(sed "s/{{task_id}}/$TASK_ID/g" "$TEMPLATE")

# Check for unresolved variables (same check as daemon)
if echo "$PROMPT" | grep -q '{{'; then
  echo "ERROR: Unresolved template variables in $TEMPLATE" >&2
  exit 1
fi

# Create output dir
mkdir -p .aetherflow/test-runs
OUTFILE=".aetherflow/test-runs/${ROLE}-${TASK_ID}-$(date +%Y%m%d-%H%M%S).jsonl"

echo "=== aetherflow test-agent ==="
echo "  role:     $ROLE"
echo "  task:     $TASK_ID"
echo "  template: $TEMPLATE"
echo "  output:   $OUTFILE"
echo "  prompt:   ${#PROMPT} chars"
echo "==========================="
echo ""

# Run opencode with the rendered prompt, streaming JSON events
# tee captures to file while we see it live
exec opencode run --format json "$PROMPT" 2>&1 | tee "$OUTFILE"
