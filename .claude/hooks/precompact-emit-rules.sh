#!/bin/bash
# PreCompact handler for llm-council.
#
# Fires before context compaction (auto or manual /compact).
# stdout is appended to the compactor's prompt as customInstructions —
# it tells the summarization model what NOT to lose.
#
# Per source: claude-code/src/services/compact/compact.ts:413-423
#   customInstructions = mergeHookInstructions(
#     customInstructions,
#     hookResult.newCustomInstructions,
#   )

set -euo pipefail

# Log invocations for debugging — PreCompact runs in a forked agent, so its
# output doesn't show in the main session transcript. Without this log, you
# can't verify the hook fired. Remove if log noise becomes a problem.
# shellcheck source=_lib/hook-common.sh
source "$(dirname "$0")/_lib/hook-common.sh"
hook_setup_logging "precompact-emit-rules.sh"

# Path is relative to this script's location (.claude/hooks/ → .claude/).
ESSENTIALS_FILE="$(dirname "$0")/../context-essentials.md"

# Read hook input (we don't use it for now, but consume to avoid SIGPIPE)
INPUT=$(cat)
echo "[$(date -Iseconds)] precompact input: $(echo "$INPUT" | head -c 200)" >> "$LOG_FILE"

if [[ -f "$ESSENTIALS_FILE" ]]; then
  cat <<EOF
When summarizing, ensure these llm-council project rules are EXPLICITLY
preserved in the summary's "Key Technical Concepts" section, even if they
were only mentioned once or implicitly:

$(cat "$ESSENTIALS_FILE")

Also preserve:
- All file paths that were read or modified during this session
- Any user feedback containing "don't", "stop", "instead", "rather"
- Active TodoWrite state
- Tech Lead approval status of any in-progress plans
- The current branch name and PR number (if any)
EOF
fi
