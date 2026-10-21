# Copilot instructions

Follow [`AGENTS.md`](../AGENTS.md) at the repository root: it is the single source of truth for layout, commands, conventions and the step-by-step guide to adding an endpoint. Copilot cannot import files, so the most important rules are repeated here.

## Key rules

1. Use stdlib `net/http` routing with Go 1.22 method patterns (`"GET /api/v1/version"`); no router frameworks.
2. Inject dependencies through constructor config structs; no globals or package-level mutable state.
3. Propagate `r.Context()` through every call and log with `slog` `*Context` methods (`InfoContext`, `ErrorContext`).
4. Wrap errors with `fmt.Errorf("...: %w", err)`; never ignore them.
5. Write JSON responses with the `writeJSON` helper in `internal/transport/http/handlers/json.go`.
6. New settings go in `internal/config`: a flag, a default and validation; each flag maps to an `APP_` env var (flag > env > default).
7. Register routes in `internal/app/handlers.go`; protected routes take the authorization middleware.
8. Do not add per-handler request metrics: `middleware.Metrics` already records them for every route.
9. Never edit `docs/` by hand (run `make swagger`), never commit `keys/` or secrets, and never enable the dev-only token endpoint (`-jwt-dev-token-endpoint`) by default.
10. Write table-driven tests with `httptest` and throwaway keys in `t.TempDir()`; `make check lint test` must pass. Use Conventional Commits.
