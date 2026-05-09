#!/bin/bash
# SessionStart handler for llm-council with matcher='compact'.
#
# Fires when a session resumes AFTER compaction. Returns JSON whose
# additionalContext is injected into the new session.
#
# Per source: claude-code/src/utils/hooks.ts:643-647
#   case 'SessionStart':
#     result.additionalContext = json.hookSpecificOutput.additionalContext

set -euo pipefail

# Log invocations for debugging. Symmetric with precompact-emit-rules.sh.
LOG_FILE="${LLM_COUNCIL_HOOK_LOG:-/tmp/llm-council-hooks.log}"
exec 2> >(tee -a "$LOG_FILE" >&2)
echo "[$(date -Iseconds)] session-restore-rules.sh invoked" >> "$LOG_FILE"

ESSENTIALS_FILE="$(dirname "$0")/../context-essentials.md"

# Consume input to avoid SIGPIPE
INPUT=$(cat)
echo "[$(date -Iseconds)] session-restore input: $INPUT" >> "$LOG_FILE"

if [[ -f "$ESSENTIALS_FILE" ]]; then
  CONTENT=$(cat "$ESSENTIALS_FILE")
  jq -n --arg ctx "$CONTENT" '{
    hookSpecificOutput: {
      hookEventName: "SessionStart",
      additionalContext: ("Session resumed after compaction. llm-council critical rules re-injected:\n\n" + $ctx)
    }
  }'
fi
