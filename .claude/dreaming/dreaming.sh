#!/bin/bash
# vmm-rada project dreaming pass.
#
# Purpose: scheduled (weekly) curation of .claude/context-essentials.md drift,
# /fix-review themes, stale plans, agent-memory health.
# Read-only — outputs report only.
#
# Schedule (cron): `0 4 * * 0  /home/val/wrk/projects/vmm-rada/vmm-rada/.claude/dreaming/dreaming.sh`
# (Sunday 04:00, 1h after user-level pass to space out API load)

set -euo pipefail

# Ensure claude/gh/jq are reachable when invoked from cron or other minimal env
export PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

# Route claude-code through Ollama (localhost:11434, Device Key auth).
# Avoids Anthropic weekly limits — see ~/wrk/common/dreaming/dreaming.sh
# for the rationale and env-vars justification.
export ANTHROPIC_AUTH_TOKEN=ollama
export ANTHROPIC_API_KEY=""
export ANTHROPIC_BASE_URL=http://localhost:11434

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REPORTS_DIR="$SCRIPT_DIR/reports"
PROMPT_FILE="$SCRIPT_DIR/dreaming-prompt.md"
WEEK="$(date +%Y-W%V)"
REPORT="$REPORTS_DIR/$WEEK.md"
LOG="$REPORTS_DIR/.dreaming.log"

mkdir -p "$REPORTS_DIR"

if [[ ! -f "$PROMPT_FILE" ]]; then
  echo "[dreaming] missing prompt file: $PROMPT_FILE" >&2
  exit 1
fi

if [[ ! -d "$PROJECT_DIR/.git" ]]; then
  echo "[dreaming] not a git repo: $PROJECT_DIR" >&2
  exit 1
fi

echo "[$(date -Iseconds)] vmm-rada dreaming pass started" >> "$LOG"

# Run from project root so relative paths in prompt work.
cd "$PROJECT_DIR"

PROMPT="$(cat "$PROMPT_FILE")
Today is $(date -I).
Current branch: $(git rev-parse --abbrev-ref HEAD).
Project root: $PROJECT_DIR.
Write the report to stdout."

# Prompt via stdin — `--allowed-tools <tools...>` is variadic and would
# otherwise consume the positional prompt argument.
echo "$PROMPT" | claude \
  --print \
  --model minimax-m3:cloud \
  --fallback-model kimi-k2.7-code:cloud \
  --allowed-tools "Read,Glob,Grep,Bash(ls:*),Bash(cat:*),Bash(wc:*),Bash(stat:*),Bash(find:*),Bash(git log:*),Bash(git diff:*),Bash(git show:*),Bash(git rev-parse:*),Bash(gh pr list:*),Bash(gh pr view:*),Bash(gh issue list:*)" \
  > "$REPORT" 2>> "$LOG"

EXIT=$?
echo "[$(date -Iseconds)] vmm-rada dreaming finished (exit=$EXIT, report=$REPORT)" >> "$LOG"

if [[ $EXIT -ne 0 ]]; then
  echo "[dreaming] non-zero exit; check $LOG" >&2
  exit "$EXIT"
fi

SIZE=$(wc -c < "$REPORT")
if [[ "$SIZE" -lt 500 ]]; then
  echo "[dreaming] WARNING: report suspiciously small ($SIZE bytes); check $REPORT" >&2
fi

echo "[dreaming] OK: $REPORT ($SIZE bytes)"

# Cron-safe safety net: verify the knowledge graph is current with the
# current commit. The post-commit git hook fires `graphify update .`
# automatically, but this catches silent failures from a fresh clone or
# an alternative machine where the hook never got installed. Output goes
# to the dreaming report — a stale graph is its own finding.
GRAPH_AGE_DAYS=""
if [[ -f graphify-out/graph.json ]]; then
  GRAPH_MTIME=$(stat -c %Y graphify-out/graph.json)
  NOW=$(date +%s)
  GRAPH_AGE_DAYS=$(( (NOW - GRAPH_MTIME) / 86400 ))
fi
if [[ -z "$GRAPH_AGE_DAYS" || "$GRAPH_AGE_DAYS" -gt 7 ]]; then
  echo "" >> "$REPORT"
  echo "## graphify freshness" >> "$REPORT"
  if [[ -z "$GRAPH_AGE_DAYS" ]]; then
    echo "- graphify-out/graph.json: missing (graphify never built or was deleted)" >> "$REPORT"
  else
    echo "- graphify-out/graph.json: $GRAPH_AGE_DAYS days old (>7 → stale; post-commit hook may have failed)" >> "$REPORT"
  fi
  echo "  Fix: run \`graphify update .\` from this repo's root." >> "$REPORT"
  echo "[dreaming] WARN: graphify graph stale or missing (age_days=${GRAPH_AGE_DAYS:-missing})" >> "$LOG"
fi
