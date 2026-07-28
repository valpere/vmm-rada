#!/bin/bash
# Stop hook: appends pattern-shaped content from this session to
# .claude/_patterns/wins.jsonl or .claude/_patterns/mistakes.jsonl.
#
# Motivation: the existing PostToolUse "consider /self-learn log" nudge is
# unreliable — vmm-rada has had it for months and its pattern files
# were stale by 34+ days before this hook was added. A reminder that
# isn't acted on is barely better than no reminder. This hook closes
# the loop by writing on session end, using the same model-fallback
# chain session-end.sh already uses (agy → opencode → raw excerpt).
#
# Failures are silent: a network timeout or non-JSON output MUST NOT
# block session end. Hooks log via _lib/hook-common.sh.

set -uo pipefail

# shellcheck source=_lib/hook-common.sh
source "$(dirname "$0")/_lib/hook-common.sh"
hook_setup_logging "session-self-learn.sh"

INPUT=$(cat)
echo "[$(date -Iseconds)] session-self-learn invoked" >> "$LOG_FILE"

PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
PATTERNS_DIR="$PROJECT_ROOT/.claude/_patterns"
WINS="$PATTERNS_DIR/wins.jsonl"
MISTAKES="$PATTERNS_DIR/mistakes.jsonl"

# Ensure target files exist (the pattern is "create empty if missing" per
# /self-learn's init step). Without these, the cheap model has nowhere
# to write and the hook silently no-ops.
mkdir -p "$PATTERNS_DIR"
[[ -f "$WINS" ]]      || : > "$WINS"
[[ -f "$MISTAKES" ]] || : > "$MISTAKES"

# Unique per-invocation scratch files — Stop can fire from multiple
# sessions in the same project concurrently, so a fixed /tmp path would
# let two invocations clobber each other's intermediate state.
MISTAKES_TMP=$(mktemp /tmp/self-learn-mistakes.XXXXXX)
WINS_TMP=$(mktemp /tmp/self-learn-wins.XXXXXX)
PARSE_ERR_TMP=$(mktemp /tmp/self-learn-parse-err.XXXXXX)
trap 'rm -f "$MISTAKES_TMP" "$WINS_TMP" "$PARSE_ERR_TMP"' EXIT

# Locate the session transcript — same logic session-end.sh uses, so the
# two hooks stay in lockstep if the location changes upstream.
TRANSCRIPT=$(echo "$INPUT" | python3 -c \
  "import json,sys; d=json.load(sys.stdin); print(d.get('transcript_path',''))" 2>/dev/null || echo "")

if [[ -z "$TRANSCRIPT" || ! -f "$TRANSCRIPT" ]]; then
  PROJECT_HASH=$(pwd | sed 's|/|-|g')
  TRANSCRIPT=$(ls -t "$HOME/.claude/projects/$PROJECT_HASH"/*.jsonl 2>/dev/null | head -1 || echo "")
fi

if [[ -z "$TRANSCRIPT" || ! -f "$TRANSCRIPT" ]]; then
  echo "[$(date -Iseconds)] session-self-learn: no transcript found, skipping" >> "$LOG_FILE"
  exit 0
fi

# Read the most recent portion of the transcript (last 500 lines is
# plenty for pattern-shape detection; the cheap model below only needs
# the tail). Skip empty transcripts early.
TAIL=$(tail -500 "$TRANSCRIPT" 2>/dev/null | grep -v '^$' || true)
if [[ -z "$TAIL" ]]; then
  echo "[$(date -Iseconds)] session-self-learn: empty transcript, skipping" >> "$LOG_FILE"
  exit 0
fi

# Ask the cheap model to extract pattern-shaped content. Strict JSON
# array of zero-or-more pattern objects is the contract — the cheap
# model returns one element per real win/mistake, or an empty array
# (most sessions), and the hook parses whatever it gets. See
# .claude/skills/self-learn/SKILL.md "LOG" for the full schema.
PROMPT=$(cat <<EOF
You are a silent post-session reviewer for an AI coding agent. Read the
transcript tail below. Identify anything that matches either of these
patterns:

1. WIN: a concrete pattern the agent did well that's worth reusing
   elsewhere — a clever solution, an efficient approach, a novel debug.
2. MISTAKE: something the agent did wrong or inefficiently enough that
   a future session should avoid it — a wrong assumption, an unnecessary
   workaround, a missed shortcut, a confusing API.

For each pattern you find, emit exactly one JSON object (no commentary,
no markdown fences, no preamble) with the schema below. Use the cheap-
model schema documented in .claude/skills/self-learn/SKILL.md "LOG".

Mistake schema:
  {"date":"<YYYY-MM-DD>","project":"vmm-rada","task":"<short>",
   "mistake":"<what went wrong>","resolution":"<how it was fixed>",
   "pattern":"<generalizable one-sentence lesson>","severity":"low|medium|high",
   "category":"api_error|wrong_assumption|missed_context|didnt_ask|prompt_quality|context_waste|skipped_planning|tooling_error|other"}

Win schema:
  {"date":"<YYYY-MM-DD>","project":"vmm-rada","task":"<short>",
   "win":"<what worked>","pattern":"<reusable lesson>","reusable_in":"<where else>",
   "had_verification":true,"used_plan_mode":false,"delegation":"subagent|manual|none",
   "decomposed":false}

If the session had no recognizable wins or mistakes, output only an
empty JSON array: []

Use today's date. Be conservative — false positives pollute the pattern
store; false negatives are recoverable next session.

TRANSCRIPT TAIL:
$TAIL
EOF
)

# Cheap-model chain (agy → opencode → raw excerpt fallback). Same chain
# session-end.sh uses. NEVER block session-end on a model failure: if
# no model responds, exit 0 and let the user do it manually via
# /self-learn log if they care.
MODEL_OUT=""
for MODEL in agy opencode; do
  if MODEL_OUT=$(echo "$PROMPT" | "$MODEL" 2>/dev/null); then
    [[ -n "$MODEL_OUT" ]] && break
  fi
done

if [[ -z "$MODEL_OUT" ]]; then
  echo "[$(date -Iseconds)] session-self-learn: model chain empty, skipping (manual /self-learn log needed if patterns exist)" >> "$LOG_FILE"
  exit 0
fi

# Strip markdown fences / prose wrapping the JSON array. Cheap models
# emit two shapes: pretty-printed (multi-line, the JSON on its own lines)
# and minified (single-line `[ {...}, {...} ]`). Both shapes are handled:
# the first branch trims to whatever lines start with `[` and end with `]`,
# the second branch keeps the raw output as-is for minified input. If
# parsing fails downstream, treat as empty array (silent skip).
if echo "$MODEL_OUT" | grep -q '^\['; then
  if echo "$MODEL_OUT" | grep -q '^\]'; then
    # Pretty-printed: select only the [...] block.
    JSON=$(echo "$MODEL_OUT" \
      | sed -n '/^\[/,/^\]/p' \
      | sed 's/^```[a-zA-Z]*$//;s/^```$//' \
      | tr -d '\r')
  else
    # Minified (single line): keep the whole output, strip CRs.
    JSON=$(echo "$MODEL_OUT" | tr -d '\r')
  fi
else
  JSON=""
fi

if [[ -z "$JSON" ]]; then
  echo "[$(date -Iseconds)] session-self-learn: no JSON array in model output, skipping" >> "$LOG_FILE"
  exit 0
fi

# Validate with python: parse the JSON array, split into
# validated mistakes / wins streams, write each to its own tmp file
# (awk would handle this but python keeps the schema validation in one
# place). Bad rows are dropped silently.
echo "$JSON" | python3 -c "
import json, sys
try:
    arr = json.loads(sys.stdin.read())
except Exception:
    sys.exit(1)
if not isinstance(arr, list):
    sys.exit(1)
required_mistake = {'date', 'project', 'task', 'mistake', 'resolution', 'pattern', 'severity', 'category'}
required_win = {'date', 'project', 'task', 'win', 'pattern', 'reusable_in'}
mistakes, wins = [], []
for item in arr:
    if not isinstance(item, dict):
        continue
    keys = set(item.keys())
    if 'mistake' in keys:
        if required_mistake.issubset(keys) and item.get('severity') in {'low','medium','high'}:
            mistakes.append(item)
    elif 'win' in keys:
        if required_win.issubset(keys):
            wins.append(item)
open('$MISTAKES_TMP','w').write('\n'.join(json.dumps(m) for m in mistakes))
open('$WINS_TMP','w').write('\n'.join(json.dumps(w) for w in wins))
" 2>"$PARSE_ERR_TMP"

if [[ $? -ne 0 ]]; then
  echo "[$(date -Iseconds)] session-self-learn: JSON validation failed: $(cat "$PARSE_ERR_TMP")" >> "$LOG_FILE"
  exit 0
fi

# Append each validated entry to its target file. Counting writes
# separately so the log shows the actual pattern-store delta. The trap
# set earlier cleans up MISTAKES_TMP/WINS_TMP/PARSE_ERR_TMP on exit.
WRITTEN=0
if [[ -s "$MISTAKES_TMP" ]]; then
  N=$(grep -c . "$MISTAKES_TMP" 2>/dev/null || echo 0)
  cat "$MISTAKES_TMP" >> "$MISTAKES"
  WRITTEN=$((WRITTEN + N))
fi
if [[ -s "$WINS_TMP" ]]; then
  N=$(grep -c . "$WINS_TMP" 2>/dev/null || echo 0)
  cat "$WINS_TMP" >> "$WINS"
  WRITTEN=$((WRITTEN + N))
fi

echo "[$(date -Iseconds)] session-self-learn: wrote $WRITTEN pattern entries" >> "$LOG_FILE"
exit 0