# AGENTS.md — go-mighty

Guidance for autonomous agents working in this repository. This is the Go backend for the **Mighty** card game: an authoritative, real-time game server with a REST/WebSocket edge, a pure rules engine, and Redis + PostgreSQL state.

Read [`readme.md`](./readme.md) for the architecture overview and [`docs/`](./docs) for API, rules, and observability details before making changes.

---

## 1. What this repository is

A single Go module (`github.com/joekhosbayar/go-mighty`, Go 1.25) with a strictly layered dependency graph. The edge speaks to clients; services orchestrate I/O; the engine is pure and deterministic; stores persist state.

| Path | Responsibility | Rule of thumb |
| --- | --- | --- |
| `cmd/server` | Process entry: config from env, dependency wiring, router, signal handling, telemetry flush. | All configuration reading lives here. |
| `internal/api` | HTTP handlers, WebSocket upgrade/relay, auth context, hardening, rate-limit middleware, health. | The only layer that serializes to clients, and therefore the only layer that redacts state per viewer. |
| `internal/service` | Orchestration: locking, version checks, store writes, event publishing, Cognito-backed auth. | Knows about I/O; contains no card rules. |
| `internal/game` | Deck, hands, phases, bidding, trick resolution, power matrix, scoring, per-player views. | Pure and deterministic — no context, no network, no clock dependence. |
| `internal/store/redis` | Hot game state (CAS writes), distributed lock, pub/sub. | Serializes `game.Game`; never applies rules. |
| `internal/store/postgres` | Append-only move ledger, game status, users, stats. | Audit truth; safe to replay. |
| `internal/infra` | Raw client constructors: Redis, Postgres, Cognito IdP. | Connection concerns only; no domain types. |
| `internal/ratelimit` | Redis Lua token bucket plus an in-process bucket for WS message rates. | Fails open on backend failure. |
| `internal/obs` | OpenTelemetry tracer/meter providers, metric instruments, zerolog setup. | Inert without `OTEL_EXPORTER_OTLP_ENDPOINT`. |
| `migrations/` | Versioned up/down SQL, applied by the `migrate` container. | Schema changes require new numbered files here. |
| `tests/e2e/` | Gherkin end-to-end suites (`integration` build tag) against a running stack. | Docker compose stack required. |
| `deploy/` | Compose stacks, Terraform, deploy scripts. | See `deploy/DEPLOY.md`. |

Dependencies point strictly downward; `internal/api` reaches state only through `internal/service`.

---

## 2. Prerequisites

- Go 1.25 (see `go.mod`).
- Docker + Docker Compose for the full stack and integration tests.
- Postgres 16 and Redis 7 (`docker compose up -d` provides both).
- `secrets/postgres_password.txt` must exist before `docker compose up -d`:
  ```bash
  cp secrets/postgres_password.txt.example secrets/postgres_password.txt
  ```
  CI does the same thing (writes a throwaway password).
- `COGNITO_POOL_ID` and `COGNITO_CLIENT_ID` are **required**; startup fails without them. For local runs, use the values CI sets.

---

## 3. Commands

### Build
```bash
# Local binary
go build ./cmd/server

# Release-style build (matches the README)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o mighty ./cmd/server
```

### Test
```bash
# Unit tests (includes the engine's Gherkin spec in internal/game)
go test ./internal/...

# Verbose, matching CI
go test -v ./internal/...

# Race detector (slow; there is a documented test-only race — see Known gaps in .claude/ARCHITECTURE.md)
go test -race ./internal/...

# End-to-end Gherkin suites against a running stack
go test -v -tags=integration ./tests/e2e/...
```

### Lint
The project uses `golangci-lint` with a deliberately strict config in `.golangci.yml` (gosec, errcheck, testifylint, paralleltest, exhaustive, revive, and more are enabled; `gofumpt` with extra rules is the formatter). Run it on changed packages before pushing:
```bash
golangci-lint run ./...
```

### Run the stack
```bash
docker compose up -d                          # postgres, redis, migrate, and the server
docker compose --profile observability up -d  # additionally ships Alloy + telemetry locally
```

Config is env-driven; see the Configuration table in [`readme.md`](./readme.md) for `PORT`, `POSTGRES_CONN`, `REDIS_ADDR`, `ALLOWED_ORIGINS`, `TRUST_PROXY_HEADERS`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `LOG_LEVEL`.

### CI
`.github/workflows/ci.yml` runs on push/PR to `main`: Go 1.25, stack via `docker compose up -d`, then `go test -v ./internal/...`. Keep that green.

---

## 4. Invariants you must not break

1. **Dependency direction.** `internal/game`, `internal/ratelimit`, `internal/obs`, and `internal/infra` import nothing else from the project. `internal/api` reaches state only through `internal/service`. Do not add downward imports.
2. **The engine is pure.** `internal/game` must stay deterministic: no `context`, no network, no clock dependence. Move validation and application happen in memory; services do the I/O.
3. **Optimistic concurrency.** Every mutating move carries `client_version`; saves are compare-and-set on the stored version. A stale client loses rather than corrupts a trick.
4. **Distributed locking.** Multi-step transitions run under a token-scoped Redis lock; release is token-checked so a timed-out holder cannot unlock a successor.
5. **Full state on the bus, redaction at the edge.** `GameEvent` carries the complete `*game.Game` internally. Outgoing events carry a `*game.GameView`; the relay decodes/re-encodes so undeclared fields are dropped rather than leaked. Never serialize full game state to clients.
6. **Two stores, two jobs.** Redis holds hot, mutable, expiring state; Postgres is the immutable append-only truth. Do not make Redis authoritative for anything that belongs in the ledger.
7. **Schema evolution lives in `migrations/`.** Add a new numbered up/down pair; never edit an already-applied migration. The `migrate` container applies them before the server starts.
8. **Observability cardinality rule.** `game_id`, `user_id`, `conn_id`, and client IPs are **span attributes and log fields only — never metric labels, never Loki labels, never span names.** This is enforced by `TestNoForbiddenMetricLabels` in `internal/obs` and exists because Grafana Cloud's free tier silently drops data past its 10,000-series cap.
9. **Observability is opt-in.** With `OTEL_EXPORTER_OTLP_ENDPOINT` unset everything in `internal/obs` is a no-op; `go test ./...` must never open sockets.
10. **WebSocket instrumentation.** Do not wrap WS routes with the `otelhttp` middleware (its wrapper breaks `http.Hijacker`). End spans explicitly on every exit path — never with `defer` inside a read loop.

---

## 5. Conventions

- **Tests live beside the code**: `*_test.go` files in the same package. Executable engine specs are Gherkin in `internal/game/features/`; end-to-end features live in `tests/e2e/features/` under `//go:build integration`.
- **Logging** is zerolog structured JSON via `obs.Log(ctx)`, which binds `trace_id`/`span_id` when a span is present. Emit exactly one log line per HTTP request (`LoggingMiddleware`).
- **Errors**: wrap with `%w`, use `errors.Is`/`errors.As`; several correctness linters are enabled, so handle or explicitly propagate errors.
- **Redaction-aware views**: when adding API surfaces, ship a deliberately narrower view type for clients (see `LobbyGameView` vs `GameView`).
- **Docs-with-code**: changes to API shapes, security controls, or deployment pipelines must update the corresponding docs in the same commit (`docs/`, `deploy/DEPLOY.md`).

---

## 6. Where to look

- [`readme.md`](./readme.md) — architecture planes, data-plane invariants, config table.
- [`docs/API_DOCUMENTATION.md`](./docs/API_DOCUMENTATION.md) + [`docs/openapi.yaml`](./docs/openapi.yaml) — HTTP/WS surface.
- [`docs/rules.md`](./docs/rules.md) — Mighty game rules as implemented.
- [`docs/SERVICE_ARCHITECTURE.md`](./docs/SERVICE_ARCHITECTURE.md) and `.claude/ARCHITECTURE.md` — runtime wiring and **known gaps** (read the gaps before touching telemetry, tests, or status handling).
- [`docs/OBSERVABILITY.md`](./docs/OBSERVABILITY.md) — traces, metrics, logs, cardinality.
- [`deploy/DEPLOY.md`](./deploy/DEPLOY.md) — build/push/deploy flow; production images must be `--platform linux/arm64`.
