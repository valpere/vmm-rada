#!/bin/bash
# Shared logging setup for vmm-rada Claude Code hooks.
#
# Usage: source this file, then call hook_setup_logging "<script-name>"
# Sets LOG_FILE (global) and redirects stderr to the log + terminal.

hook_setup_logging() {
  local script_name="$1"
  LOG_DIR="${VMM_RADA_HOOK_LOG_DIR:-${HOME}/.cache/vmm-rada}"
  mkdir -p "$LOG_DIR" && chmod 700 "$LOG_DIR"
  LOG_FILE="$LOG_DIR/hooks.log"
  exec 2> >(tee -a "$LOG_FILE" >&2)
  echo "[$(date -Iseconds)] $script_name invoked" >> "$LOG_FILE"
}
