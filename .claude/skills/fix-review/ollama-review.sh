#!/usr/bin/env bash
# CLI-tier helper for /fix-review.
# Reads the review prompt from stdin, calls a local Ollama model via the
# OpenAI-compat endpoint, and writes the model's raw content to stdout.
# The parent skill expects a JSON array on stdout; non-JSON output is treated
# as 0 findings for this round (safe degradation).
#
# Usage (in config.yaml cli.cmd):
#   bash .claude/skills/fix-review/ollama-review.sh <model>

set -euo pipefail

MODEL="${1:?usage: $0 <model>}"
BASE_URL="${OLLAMA_HOST:-http://localhost:11434}"
PROMPT=$(cat)

curl -sf --max-time 180 \
  "${BASE_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
        --arg m  "$MODEL" \
        --arg sys "Your entire response MUST be a raw JSON array — nothing else. Start with [ and end with ]. No prose, no markdown fences." \
        --arg usr "$PROMPT" \
        '{model:$m,messages:[{role:"system",content:$sys},{role:"user",content:$usr}],stream:false,max_tokens:4096}')" \
  | jq -r '.choices[0].message.content // "[]"'
