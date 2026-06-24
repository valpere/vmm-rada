---
type: bug
priority: p2
labels: bug, p2: medium, frontend
github_issue: ""
debt: quick-fix
effort: xs
---

# Stage2.jsx: route LLM content through <Markdown> wrapper

## Dreaming reference
§1.a of 2026-W25 report. Rule 4 of context-essentials: "`react-markdown` is the only renderer for LLM output." Stage2.jsx bypasses this contract in 4 places.

## Summary

`frontend/src/components/Stage2.jsx` renders LLM-generated text as bare JSX string children in 4 spots, bypassing the `<Markdown>` wrapper that every other Stage component uses (Stage0, Stage1, Stage3, ChatInterface all import `./Markdown`).

The risk is not XSS (React escapes string children; no `dangerouslySetInnerHTML` present) but markdown fidelity — bold, code blocks, lists in model output render as literal symbols instead of formatted text.

## Affected lines

- `:332` — `{revision.critique}` inside `<span className="debate-critique-text">`
- `:336` — `{revision.content}`
- `:426` — `{result.content}`
- `:455` — `{aggregator.content}`

## Approach

1. Add `import Markdown from './Markdown';` at the top of `Stage2.jsx`.
2. Replace each bare string render with `<Markdown>{field}</Markdown>`.
   - `:332`: replace `<span className="debate-critique-text">{revision.critique}</span>` → `<Markdown>{revision.critique}</Markdown>` (the span is only there to hold text; the Markdown wrapper renders a block — check if span is needed for CSS or can be dropped).
   - `:336`: `{revision.content}` → `<Markdown>{revision.content}</Markdown>`
   - `:426`: `{result.content}` → `<Markdown>{result.content}</Markdown>`
   - `:455`: `{aggregator.content}` → `<Markdown>{aggregator.content}</Markdown>`
3. Run `npm run build` + smoke-test in the browser (debate and MoA strategies exercise these paths).

## Files to change

- `frontend/src/components/Stage2.jsx`

## Acceptance criteria

- All four fields render through `<Markdown>` — no bare string children for LLM content.
- `npm run build` passes.
- Strategy outputs that contain markdown (bold, code, lists) display correctly in the UI.
- No `dangerouslySetInnerHTML` introduced.
