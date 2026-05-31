---
name: cors-allowed-methods
description: CORS middleware in handler.go hardcodes allowed methods — any new HTTP verb (DELETE/PATCH/PUT) requires updating it
type: project
---

The `wrap()` CORS middleware in `internal/api/handler.go` hardcodes
`Access-Control-Allow-Methods: "GET, POST, OPTIONS"`. Allowed CORS origins are
a hardcoded allowlist (`allowedOrigins`: localhost:5173, localhost:3000).

**Why:** Any plan that adds an endpoint using a verb beyond GET/POST (DELETE,
PATCH, PUT) will pass Go-side tests but fail in the browser at CORS preflight,
because the middleware never advertises the new method. This is easy to miss in
review because handler unit tests bypass the browser preflight.

**How to apply:** When reviewing a plan that introduces a new HTTP verb, REQUIRE
the plan to also update the `Access-Control-Allow-Methods` header in `wrap()`.
Treat its omission as a Layer 2 blocking issue.
