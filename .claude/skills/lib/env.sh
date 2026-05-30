#!/usr/bin/env bash
# lib/env.sh — load named keys from .env
#
# Usage:
#   source .claude/skills/lib/env.sh
#   load_env_key AI_PROVIDER_API_KEY          # exports AI_PROVIDER_API_KEY; warns if missing
#   load_env_key AI_PROVIDER_API_KEY .env.local  # override env file
#
# Strips trailing whitespace and surrounding quotes to prevent invisible auth failures.

load_env_key() {
  local key="$1"
  local env_file="${2:-.env}"
  local value
  value=$(grep "^${key}=" "$env_file" 2>/dev/null | cut -d= -f2- | sed 's/^["'"'"']//;s/["'"'"']$//' | sed 's/[[:space:]]*$//')
  if [ -z "$value" ]; then
    echo "WARNING: ${key} not found in ${env_file}" >&2
  else
    export "$key"="$value"
  fi
}
