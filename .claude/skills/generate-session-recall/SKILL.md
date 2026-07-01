---
name: generate-session-recall
description: "Installs per-project semantic session recall: session-index.sh (Stop hook) + session-recall.sh (SessionStart hook) + /recall skill. Complements session-end. Requires session-indexer binary. Usage: /generate-session-recall"
---

# Skill: /generate-session-recall
# Install Per-Project Semantic Session Recall

Installs the session-recall system into the current project. Run once per project.

What this does:
1. Checks `session-indexer` binary is available
2. Installs `session-index.sh` (Stop hook — mines JSONL → `.claude/sessions.db`)
3. Installs `session-recall.sh` (SessionStart hook — semantic search injection)
4. Updates `.claude/settings.local.json`
5. Adds `sessions.db` to `.gitignore`
6. Writes `.claude/skills/session-recall/SKILL.md`

---

## DISCOVERY

### Step 1 — Check existing setup

```bash
echo "=== session-indexer binary ===" && command -v session-indexer && session-indexer --version 2>/dev/null || echo "(not found)"
echo "=== existing hooks ===" && ls .claude/hooks/session-index.sh .claude/hooks/session-recall.sh 2>/dev/null || echo "(not installed)"
echo "=== existing db ===" && ls -lh .claude/sessions.db 2>/dev/null || echo "(no db yet)"
echo "=== session-end installed ===" && ls .claude/hooks/session-end.sh 2>/dev/null || echo "(not installed)"
```

If `session-indexer` is not found, stop and report:
```
session-indexer not found in PATH.

Build and install:
  cd ~/wrk/projects/session-indexer/session-indexer
  go build -o session-indexer .
  sudo mv session-indexer /usr/local/bin/

Then re-run /generate-session-recall.
```

### Step 2 — Get project name (for log path in troubleshooting)

```bash
basename "$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
```

---

## INSTALL

### Step 1 — Copy hook scripts

Source: `~/wrk/common/skills/session-recall/hooks/`

```bash
SRC=~/wrk/common/skills/session-recall/hooks

mkdir -p .claude/hooks/_lib

cp "$SRC/session-index.sh"  .claude/hooks/session-index.sh
cp "$SRC/session-recall.sh" .claude/hooks/session-recall.sh
chmod +x .claude/hooks/session-index.sh .claude/hooks/session-recall.sh

# Copy _lib only if not present (don't overwrite existing hook infrastructure)
[[ ! -f .claude/hooks/_lib/hook-common.sh ]] && \
  cp ~/wrk/common/skills/session-end/hooks/_lib/hook-common.sh \
     .claude/hooks/_lib/hook-common.sh
```

Verify syntax:
```bash
bash -n .claude/hooks/session-index.sh  && echo "session-index.sh OK"
bash -n .claude/hooks/session-recall.sh && echo "session-recall.sh OK"
```

### Step 2 — Update settings.local.json

Read current `.claude/settings.local.json` (or `{}`), merge in the two new hooks,
write back. Do not remove any existing hooks.

```python
import json, os

path = '.claude/settings.local.json'
current = json.load(open(path)) if os.path.exists(path) else {}
hooks = current.setdefault('hooks', {})

new_stop = {"hooks": [{"type": "command", "command": "bash .claude/hooks/session-index.sh", "timeout": 60, "statusMessage": "Indexing session..."}]}
new_start = {"hooks": [{"type": "command", "command": "bash .claude/hooks/session-recall.sh", "timeout": 15, "statusMessage": "Recalling relevant context..."}]}

stop_cmds = [h.get('command','') for entry in hooks.get('Stop',[]) for h in entry.get('hooks',[])]
if 'session-index.sh' not in ' '.join(stop_cmds):
    hooks.setdefault('Stop', []).append(new_stop)

start_cmds = [h.get('command','') for entry in hooks.get('SessionStart',[]) for h in entry.get('hooks',[])]
if 'session-recall.sh' not in ' '.join(start_cmds):
    hooks.setdefault('SessionStart', []).append(new_start)

json.dump(current, open(path, 'w'), indent=2)
print('settings.local.json updated')
```

### Step 3 — Update .gitignore

If `.gitignore` exists and does not already contain `sessions.db`, append:
```
# Session recall index — per-project, local-only
.claude/sessions.db
```

### Step 4 — Write project-specific skill

Copy `~/wrk/common/skills/session-recall/SKILL.md` to
`.claude/skills/session-recall/SKILL.md`.

No customisation needed — the skill is already project-agnostic (uses
`git rev-parse --show-toplevel` to locate the DB at runtime).

---

## REPORT

```
✓ Session recall installed for {project-name}

Hooks:
  .claude/hooks/session-index.sh   — Stop hook (indexes JSONL → .claude/sessions.db)
  .claude/hooks/session-recall.sh  — SessionStart hook (semantic context injection)

Settings:  .claude/settings.local.json ✓
Gitignore: .claude/sessions.db ignored ✓
Skill:     .claude/skills/session-recall/SKILL.md ✓

session-indexer: {version}
Search:    bge-m3 embeddings (if ollama pull bge-m3 done) / BM25 FTS5 fallback

Next steps:
  1. Restart Claude Code to activate hooks
  2. End a session — session-index.sh will mine it into .claude/sessions.db
  3. Use /recall <query> to search past sessions manually

To enable vector search (better recall quality):
  ollama pull bge-m3
  session-indexer embed --db .claude/sessions.db   # backfill existing sessions

Note: session-recall complements session-end (not a replacement).
If session-end is not installed, run /generate-session-end first.
```
