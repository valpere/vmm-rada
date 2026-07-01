#!/bin/bash
# SessionStart hook: injects the most recent entry from .claude/session-log.md.
#
# Skips if the file doesn't exist or is empty. Rotation is count-based
# (last 10 entries) — no age filtering here.

set -euo pipefail

# shellcheck source=_lib/hook-common.sh
source "$(dirname "$0")/_lib/hook-common.sh"
hook_setup_logging "session-last.sh"

INPUT=$(cat)
echo "[$(date -Iseconds)] session-last invoked" >> "$LOG_FILE"

LOG="$(dirname "$0")/../session-log.md"

if [[ ! -f "$LOG" ]]; then
  echo "[$(date -Iseconds)] session-last: no session-log.md, skipping" >> "$LOG_FILE"
  exit 0
fi

# Extract the last ## YYYY-MM-DD entry
LAST_ENTRY=$(python3 - "$LOG" <<'PYEOF'
import sys, re

with open(sys.argv[1]) as f:
    content = f.read()

parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', content)
entries = [p.strip() for p in parts if p.strip()]
if entries:
    print(entries[-1])
PYEOF
)

if [[ -z "$LAST_ENTRY" ]]; then
  echo "[$(date -Iseconds)] session-last: empty log, skipping" >> "$LOG_FILE"
  exit 0
fi

# Parse date from entry header (## YYYY-MM-DD) — used only for the age label
ENTRY_DATE=$(echo "$LAST_ENTRY" | grep -oP '^## \K\d{4}-\d{2}-\d{2}' || echo "")
if [[ -n "$ENTRY_DATE" ]]; then
  ENTRY_TS=$(date -d "$ENTRY_DATE" +%s 2>/dev/null || echo 0)
  NOW=$(date +%s)
  AGE=$(( NOW - ENTRY_TS ))
  AGE_DAYS=$(( AGE / 86400 ))
  AGE_HOURS=$(( (AGE % 86400) / 3600 ))
  AGE_LABEL="${AGE_DAYS}d ${AGE_HOURS}h ago"
  [[ $AGE_DAYS -eq 0 ]] && AGE_LABEL="${AGE_HOURS}h ago"
else
  AGE_LABEL="(date unknown)"
fi

echo "[$(date -Iseconds)] session-last: injecting last entry ($AGE_LABEL)" >> "$LOG_FILE"

jq -n \
  --arg ctx "$LAST_ENTRY" \
  --arg age "$AGE_LABEL" \
  '{
    hookSpecificOutput: {
      hookEventName: "SessionStart",
      additionalContext: ("Previous session context (" + $age + "):\n\n" + $ctx)
    }
  }'
