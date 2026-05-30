#!/usr/bin/env bash
# lib/rest.sh — thin REST helper for skill scripts and agent delegation
#
# Usage:
#   source .claude/skills/lib/rest.sh
#
#   RESPONSE=$(rest_post "$URL" "$PAYLOAD" "$API_KEY")
#   RESPONSE=$(rest_post "$URL" "$PAYLOAD" "$API_KEY" "Token")  # custom auth scheme
#
# rest_post writes the raw response body to stdout.
# On HTTP error or connection failure it writes an error message to stderr
# and returns exit code 1.
#
# Set REST_TIMEOUT (seconds) to override the default of 120.

rest_post() {
  local url="$1"
  local payload="$2"
  local api_key="${3:-}"
  local auth_scheme="${4:-Bearer}"
  local timeout="${REST_TIMEOUT:-120}"

  local -a curl_args=(-sS --max-time "$timeout" -H "Content-Type: application/json" -d "$payload")
  [ -n "$api_key" ] && curl_args+=(-H "Authorization: ${auth_scheme} ${api_key}")

  local response curl_err exit_code errfile
  errfile=$(mktemp)
  response=$(curl "${curl_args[@]}" "$url" 2>"$errfile")
  exit_code=$?
  curl_err=$(cat "$errfile")
  rm -f "$errfile"

  if [ $exit_code -ne 0 ]; then
    echo "ERROR: REST POST to ${url} failed (curl exit ${exit_code}): ${curl_err}" >&2
    return 1
  fi

  printf '%s' "$response"
}

# ---------------------------------------------------------------------------
# OpenRouter — https://openrouter.ai/api/v1/chat/completions
# OpenAI-compatible API. Response shape: { "choices": [{ "message": { "content": "..." } }] }
# ---------------------------------------------------------------------------

# Build an OpenRouter chat payload via jq (handles special characters safely).
# Usage: PAYLOAD=$(openrouter_payload "$MODEL" "$PROMPT")
openrouter_payload() {
  local model="$1"
  local prompt="$2"
  jq -n --arg model "$model" --arg prompt "$prompt" \
    '{model: $model, messages: [{role: "user", content: $prompt}], stream: false}'
}

# Build an OpenRouter payload with a system prompt.
# Usage: PAYLOAD=$(openrouter_payload_sys "$MODEL" "$SYSTEM" "$USER")
openrouter_payload_sys() {
  local model="$1"
  local system="$2"
  local user="$3"
  jq -n --arg model "$model" --arg system "$system" --arg user "$user" \
    '{model: $model, messages: [{role: "system", content: $system}, {role: "user", content: $user}], stream: false}'
}

# Extract the assistant message content from an OpenRouter response.
# Usage: CONTENT=$(openrouter_content "$RESPONSE")
openrouter_content() {
  printf '%s' "$1" | jq -r '.choices[0].message.content // empty'
}

# Convenience: build payload + POST + extract content in one call.
# Usage: CONTENT=$(openrouter_ask "$MODEL" "$PROMPT")
# Requires AI_PROVIDER_API_KEY to be exported.
openrouter_ask() {
  local model="$1"
  local prompt="$2"
  local payload response
  payload=$(openrouter_payload "$model" "$prompt")
  response=$(rest_post "https://openrouter.ai/api/v1/chat/completions" "$payload" "$AI_PROVIDER_API_KEY")
  openrouter_content "$response"
}
