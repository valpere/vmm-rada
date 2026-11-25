---
name: sessions-db-growth-policy
description: Decision record for .claude/sessions.db growth (issue #344) — no rotation policy adopted, growth is discrete per session-Stop, not continuous
type: project
---

`.claude/sessions.db` (session-recall's local index) grows via
`.claude/hooks/session-index.sh`, registered on the **`Stop`** hook in
`.claude/settings.local.json` — it writes once per session end, not
continuously during a session. A single long-running session does not grow
the file at all until it stops; growth is proportional to how many sessions
end, not to how long any one session runs.

**Data points:** 131.6 MB (2026-08-23, W34 audit report) → 138.3 MB same
day (audit triage, several sessions/`/exit`s later) → 139.3 MB
(~1 hour after that, one more session-end) → still 139.3 MB the next day
(2026-08-24) after one very long continuous session with no `Stop` event yet.
~565K→568K rows. Growth per session-end event: roughly hundreds of KB to a
few MB.

**Decision (2026-08-24, resolving issue #344): no action.** Local disk is
cheap, the file is gitignored (zero repo-footprint cost), and the per-session
growth rate is small enough that even at this project's heaviest observed
usage it would take a very long time to become an actual problem. Revisit
only if the file exceeds roughly 500MB–1GB, or if `session-recall`
reads/writes start showing observable latency — neither has happened.

**Why:** The original concern (W34 audit §7) assumed continuous/unbounded
growth without checking the actual write trigger. Once the trigger is known
to be discrete and infrequent (one `Stop` event, not a per-tool-call or
per-message write), the "I/O pressure" worry loses its basis — this file
writes maybe a few times a day at most, not thousands of times.

**How to apply:** If a future audit re-flags `sessions.db` size,
check this decision first before re-opening the investigation — re-open only
if the file has actually crossed the ~500MB–1GB threshold or a real
performance symptom is observed, not on size alone.
