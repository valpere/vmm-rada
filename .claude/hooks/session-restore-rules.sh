#!/bin/bash
# SessionStart handler for vmm-rada with matcher='compact'.
#
# Fires when a session resumes AFTER compaction. Returns JSON whose
# additionalContext is injected into the new session.
#
# Per source: claude-code/src/utils/hooks.ts:643-647
#   case 'SessionStart':
#     result.additionalContext = json.hookSpecificOutput.additionalContext

set -euo pipefail

# Log invocations for debugging. Symmetric with precompact-emit-rules.sh.
# shellcheck source=_lib/hook-common.sh
source "$(dirname "$0")/_lib/hook-common.sh"
hook_setup_logging "session-restore-rules.sh"

# Path is relative to this script's location (.claude/hooks/ → .claude/).
ESSENTIALS_FILE="$(dirname "$0")/../context-essentials.md"

# Consume input to avoid SIGPIPE
INPUT=$(cat)
echo "[$(date -Iseconds)] session-restore input: $(echo "$INPUT" | head -c 200)" >> "$LOG_FILE"

if [[ -f "$ESSENTIALS_FILE" ]]; then
  jq -n --arg ctx "$(cat "$ESSENTIALS_FILE")" '{
    hookSpecificOutput: {
      hookEventName: "SessionStart",
      additionalContext: ("Session resumed after compaction. vmm-rada critical rules re-injected:\n\n" + $ctx)
    }
  }'
fi
