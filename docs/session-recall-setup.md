# Session Recall — Setup Guide

Per-project session context injection for Claude Code.
No external services, no ChromaDB, no vectors — just files.

## What this does

- **Stop hook** writes/updates `.claude/session-log.md` at session end
- **SessionStart hook** reads the last entry and injects it into the next session
- **`/session-end` skill** generates a high-quality summary on demand
- Result: when you open a project after days/weeks, Claude knows what you were doing

## session-log.md format

Entries are appended one per day, oldest first. If you open/close Claude Code
multiple times in one day, the existing today-entry is replaced (not duplicated).
After 10 day-entries, the oldest is dropped.

```markdown
## 2026-06-24

### Що зробили
- ...

### Поточний стан
- ...

### Наступні кроки
- ...

## 2026-06-25

### Що зробили
- ...
```

## Files involved

```
.claude/
  hooks/
    session-end.sh       # Stop hook — writes/updates session-log.md
    session-last.sh      # SessionStart hook — injects last entry
  skills/
    session-end/
      SKILL.md           # /session-end skill — manual high-quality summary
  settings.local.json    # wires the hooks (gitignored)
  session-log.md         # the log file itself (gitignored)
```

## LLM fallback chain (Stop hook)

The Stop hook tries each in order until one succeeds:

1. **`agy -p`** — Gemini 3.5 Flash (Low → Medium → High) → Gemini 3.1 Pro (Low → High)
2. **`opencode run --format json`** — available `ollama/*:cloud` models in order
3. **Raw excerpt** — last 60 lines of transcript, no LLM

The `/session-end` skill (run manually) uses Claude directly (current session
context) and always produces the best result.

## Setup for a new project

### 0. Recommended: use /generate-session-end

The easiest path — inject the generate skill and run it:

```bash
PROJECT=<project>
mkdir -p "$PROJECT/.claude/skills/generate-session-end"
cp ~/wrk/common/skills/session-end/generate/SKILL.md \
   "$PROJECT/.claude/skills/generate-session-end/SKILL.md"
```

Then in Claude Code inside the project: `/generate-session-end`

It handles steps 1–4 automatically. Manual steps below for reference.

---

### 1. Copy hooks

```bash
SRC=~/wrk/common/skills/session-end/hooks
DST=<project>/.claude/hooks

mkdir -p "$DST/_lib"
cp "$SRC/session-end.sh" "$DST/"
cp "$SRC/session-last.sh" "$DST/"
[[ ! -f "$DST/_lib/hook-common.sh" ]] && cp "$SRC/_lib/hook-common.sh" "$DST/_lib/"
chmod +x "$DST/session-end.sh" "$DST/session-last.sh"
```

### 2. Copy the skill

```bash
mkdir -p <project>/.claude/skills/session-end
cp ~/wrk/common/skills/session-end/SKILL.md \
   <project>/.claude/skills/session-end/SKILL.md
```

### 3. Create or update settings.local.json

`.claude/settings.local.json` (gitignored):

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/session-end.sh",
            "timeout": 60,
            "statusMessage": "Writing session summary..."
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/session-last.sh",
            "timeout": 10,
            "statusMessage": "Loading previous session context..."
          }
        ]
      }
    ]
  }
}
```

If the project already has a `settings.local.json`, merge the hooks arrays.

### 4. Update .gitignore

```gitignore
# Session recall — per-project, local-only context log
.claude/session-log.md
```

### 5. Verify

```bash
bash -n .claude/hooks/session-end.sh
bash -n .claude/hooks/session-last.sh
```

Restart Claude Code. The next session stop will generate `session-log.md`.

---

## Usage

### Manual (best quality) — `/session-end`

Invoke `/session-end` before switching projects or closing Claude Code.
Claude reads the current conversation and writes a structured entry.
The Stop hook sees the file was modified recently and skips auto-summary.

### Automatic — Stop hook

Runs at every session end. Tries `agy` → `opencode` → raw fallback.
Same-day entries are replaced, not duplicated.
Rotates to 10 most recent days.

### On next session open

The last entry is injected as `additionalContext`:

```
Previous session context (2d 3h ago):

## 2026-06-25

### Що зробили
- ...
```

Injection is skipped only if `session-log.md` doesn't exist. Rotation is
count-based (last 10 entries kept) — no age filtering.

---

## Troubleshooting

**`session-log.md` not being written:**
- Check `~/.cache/<project-slug>/hooks.log`
- Verify `agy` is on PATH: `which agy`
- Test manually: `bash .claude/hooks/session-end.sh <<< '{}'`

**`opencode` models failing:**
- Run `opencode models ollama` to see available cloud models
- Update the `models` array in `session-end.sh` to match

**Injection not firing:**
- Check hooks.log for `session-last.sh` lines
- Verify `jq` is installed: `which jq`
- Verify file exists: `ls -la .claude/session-log.md`
