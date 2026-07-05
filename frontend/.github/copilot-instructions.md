# Copilot Repository Instructions

## Project

React 19 + Vite single-page application for **VMM Rada** — a 3-stage deliberation system where multiple LLMs answer a question, peer-review each other anonymously, and a Chairman model synthesizes a final answer.

The frontend communicates with a Go backend running on port 8001 via REST and Server-Sent Events (SSE). See `docs/api-contract.md` and `docs/streaming.md` for the API contract.

## Commands

```bash
npm install       # install dependencies
npm run dev       # dev server at http://localhost:5173
npm run build     # production build
npm run lint      # ESLint
```

There is no test suite.

## Architecture

- **`src/api.js`** — single API client; `API_BASE` points to the Go backend
- **`src/App.jsx`** — all application state; no Redux or Context API
- **`src/components/`** — `Sidebar`, `ChatInterface`, `Stage1`, `Stage2`, `Stage3`

Assistant messages are built progressively during SSE streaming. Each stage (`stage1`, `stage2`, `stage3`) starts as `null` and is filled as events arrive. `metadata` (label_to_model, aggregate_rankings) is ephemeral — returned only during streaming, not persisted by the backend.

## Conventions

- No TypeScript; plain JavaScript (ESM)
- Each component has a co-located CSS file
- All model responses rendered via `react-markdown`
- Branch protection is active — all changes require a pull request
