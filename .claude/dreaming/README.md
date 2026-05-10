# Dreaming — llm-council project

Project-specific dreaming pass. Виявляє drift від `context-essentials.md`,
recurring `/fix-review` теми, stale plans, agent-memory health.

## Запуск

Manually:
```bash
~/wrk/projects/llm-council/llm-council/.claude/dreaming/dreaming.sh
```

Scheduled — **systemd user timer** (recommended, catches up missed runs):

```ini
# ~/.config/systemd/user/dreaming-llm-council.service
[Service]
Type=oneshot
ExecStart=/home/val/wrk/projects/llm-council/llm-council/.claude/dreaming/dreaming.sh
# Use the script's own log directory (~/.cache/llm-council/, mode 0700)
# rather than /tmp — /tmp is world-readable and could leak secrets if the
# pass ever logs prompt content, env values, or stack traces.
StandardOutput=append:%h/.cache/llm-council/dreaming-systemd.log
StandardError=append:%h/.cache/llm-council/dreaming-systemd.log

# ~/.config/systemd/user/dreaming-llm-council.timer
[Timer]
OnCalendar=Sun 04:00          # Nominal — комп зазвичай вимкнений
Persistent=true                # Catch up at next login
RandomizedDelaySec=5min        # Spread bursty boot-time catchups

[Install]
WantedBy=timers.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now dreaming-llm-council.timer
```

**Чому НЕ cron:** cron не догоняє пропущених runs — якщо комп вимкнений
у Sun 04:00, run просто втрачається. systemd `Persistent=true` запам'ятовує
і фаєриться на наступному login.

## Що шукає

1. **Context-essentials drift** — порушення immutable rules у recent commits
   - Грепає `--no-verify`, raw HTML, state writes outside App.jsx, etc.
2. **Recurring `/fix-review` themes** — читає PR-коментарі за останні 15 PR
   - Якщо одна тема повторюється у 3+ PR → кандидат на нове правило
3. **Stale plans** — `.claude/plans/*.md` старші 14 днів
4. **Agent-memory health** — застарілі / дублюючі / суперечливі memory
5. **Skill / agent inventory** — невикористані skills, overlapping responsibilities

## Звіти

Зберігаються в `reports/YYYY-W##.md`. Track-аються в git як audit trail.

`reports/.dreaming.log` — журнал запусків (gitignored).

## Workflow читання

Понеділок ранок:
1. `cat .claude/dreaming/reports/$(date +%Y-W%V).md`
2. Для high-confidence drift items — створити issue або одразу fix
3. Для recurring fix-review themes — додати рядок у `context-essentials.md` або обговорити з командою
4. Для stale plans — або підняти, або `git rm`

## Чим відрізняється від /revival

| | /revival | dreaming |
|---|----------|----------|
| Тригер | On-demand | Scheduled (cron) |
| Scope | Health snapshot | Pattern detection across time |
| Вхід | Структура зараз | Recent commits + PR comments |
| Output | Snapshot діагноз | Trend report |

Доповнюють одне одне. /revival — "як я зараз?", dreaming — "що в мене
накопичилось?"

## Cost

~$0.5-1 per pass (Opus, ~30-60K input tokens, ~5-10K output).
Тижневий запуск → ~$2-4/місяць. Щоденний — ~$15-30/місяць (overkill).

## Related

- Wiki: `~/Documents/llm-wiki/wiki/dreaming.md` (загальний concept)
- User-level: `~/wrk/common/dreaming/` (cross-project Claude Code memory)
- Wiki dreaming: `~/Documents/llm-wiki/wiki/_meta/dreaming/`
