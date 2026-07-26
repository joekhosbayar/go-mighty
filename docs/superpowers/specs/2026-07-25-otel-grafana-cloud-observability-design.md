# OpenTelemetry + Grafana Cloud Observability — Mighty (go-mighty)

**Date:** 2026-07-25
**Status:** All sections approved — awaiting final user review
**Budget:** $0 incremental (Grafana Cloud free tier; no instance resize — see Constraints)

## Goals

- Add metrics, logs, and traces to the existing production stack using OpenTelemetry and Grafana Cloud's free tier.
- Alert on real failure modes, routed to a Discord webhook.
- Fit entirely within the existing `t4g.small` (2 GB) instance — no resize.
- Change nothing about the existing out-of-band CloudWatch alarms. This design **adds a layer and deletes nothing.**

## Motivation

Current telemetry is dead-box detection only:

- **Logs:** zerolog structured JSON → stdout → Docker `json-file`, 10 MB × 3. Nothing leaves the box; a container restart loses them.
- **Metrics:** none. No `/metrics` endpoint, no OTel dependencies in `go.mod`.
- **Traces:** none.
- **Alerting** (`deploy/terraform/alarms.tf`): EC2 `StatusCheckFailed`, Route 53 HTTPS probe on `/healthz`, and a $25 forecast / $30 actual budget tripwire — all → SNS email.

So today you learn that the server is down. You do not learn that WebSocket handshakes are being rejected, that Postgres is queueing connections, that the box is swapping, or why a game desynced.

There is no triggering incident. This is preemptive work, which means YAGNI applies hard — the risk is building a three-pillar stack nobody opens.

## Decisions Made

| Decision | Choice | Why |
|---|---|---|
| Scope | Go service + host/container + Postgres/Redis | Frontend (web/Electron/Swift) explicitly out of scope — different problem shape, arguably Sentry's job |
| Pillars | Metrics + logs + traces | User choice. Tracing surface deliberately kept to entry → service → DB/Redis; game internals not instrumented |
| Transport | Alloy as single gateway | One credential set, on-box buffering, app stays vendor-neutral pointing at localhost |
| Alert routing | Discord webhook | Fast, mobile-visible, durable incident timeline |
| Config management | Hybrid: alert rules in Terraform, dashboards as exported JSON | Alert rules are small, structured, and security-relevant; dashboard-as-HCL friction stops people updating dashboards |
| Postgres tracing | `XSAM/otelsql` wrapping `database/sql` | Repo uses `lib/pq` + `database/sql`; avoids a `pgx` migration |
| Redis tracing | `redisotel` hook on `go-redis/v9` | One line; also yields pool metrics |
| WebSocket tracing | Handshake span + span per inbound message | A span per connection would stay open for a whole game — useless in Tempo, a leak in the SDK |
| Container metrics (cadvisor) | **Starts disabled**, enabled only on measured headroom | Heaviest component (~100 MB + steady CPU), least marginal value given Go runtime + host metrics |
| Existing CloudWatch alarms | Keep unchanged | Out-of-band; they work precisely when the in-band path is dead |
| Scrape interval | 60s | Traffic doesn't warrant 15s; 4× cut in CPU, network, and Alloy working set |

## Constraints

**Memory is the binding constraint.** `t4g.small` = 2 vCPU / 2 GB, plus 2 GB swap. Rough current usage: postgres ~200 MB, mighty ~80 MB, caddy ~30 MB, redis ~30 MB.

Resizing is not available as an escape hatch: `t4g.medium` adds ~$12/mo against a budget alarm set at $25 forecast / $30 actual. Everything must fit in the existing 2 GB.

**Free-tier ceilings:** 10k metric series, 50 GB logs, 50 GB traces, 14-day retention.

## Architecture

```
EC2 t4g.small (2 GB) ── docker compose
┌───────────────────────────────────────────────────┐
│  caddy ──► mighty ──► postgres                    │
│              │    └─► redis                       │
│              │                                    │
│              │ OTLP/gRPC → alloy:4317             │
│              ▼                                    │
│            alloy ◄── /var/run/docker.sock  (logs, │
│              │       container metrics)           │
│              │   ◄── unix exporter    (host)      │
│              │   ◄── postgres exporter            │
│              │   ◄── redis exporter               │
└──────────────┼────────────────────────────────────┘
               │ HTTPS + basic auth (creds from SSM Parameter Store)
               ▼
        Grafana Cloud:  Prometheus │ Loki │ Tempo
               │
               └─► Alert rules ─► Discord webhook
```

**Trust boundary.** `alloy` is the only container holding Grafana Cloud credentials and the only egress path. `mighty` exports to `alloy:4317` over the compose network and never learns the vendor exists. Alloy's OTLP receiver binds to the compose network only — it is not published to the host.

**Credentials.** Seven new SSM parameters — six endpoint/identity values plus one access-policy token — following the existing `remote-deploy.sh` fetch-into-`.env` pattern. No new secret mechanism.

| Parameter | Purpose |
|---|---|
| `/mighty/grafana/otlp_endpoint` | OTLP gateway URL (app traces + metrics) |
| `/mighty/grafana/otlp_instance_id` | OTLP basic-auth username |
| `/mighty/grafana/prom_url` | Prometheus remote-write URL (scraped infra metrics) |
| `/mighty/grafana/prom_user_id` | Prometheus basic-auth username |
| `/mighty/grafana/loki_url` | Loki push URL |
| `/mighty/grafana/loki_user_id` | Loki basic-auth username |
| `/mighty/grafana/token` | Access-policy token, scoped `metrics:write, logs:write, traces:write` |

Grafana Cloud uses a distinct user/instance ID per backend, which is why this is six endpoint/identity values rather than one URL and one key. The single token carries all three write scopes.

**Terraform must not read these parameters.** Only `remote-deploy.sh` consumes
them, reading from SSM on the box at deploy time via the instance role.
Declaring them in Terraform — even as `data` sources — writes their values,
including the access token, in plaintext into `terraform.tfstate`, which is
local and unencrypted in this project. A secret should be read by the thing
that uses it, as late as possible; routing it through a tool that only passes
it along adds a copy at rest and buys nothing.

### Failure modes

| Failure | Behavior | Coverage |
|---|---|---|
| Grafana Cloud unreachable | Alloy buffers (metrics WAL, retry queues for logs/traces) and drains on recovery. App unaffected — OTLP export failure is non-fatal by design. | Bounded on-disk queues (see below) |
| Alloy dies | Telemetry stops **silently** — the dangerous one | Grafana-side no-data alert on the host heartbeat metric |
| Box is dead | Alloy cannot report it | Existing CloudWatch `StatusCheckFailed` + Route 53 `/healthz` alarms, unchanged |
| Alloy queues fill the disk | Would take the game down | WAL and retry queues explicitly bounded in config. **Observability must not become the outage.** |

### New and changed files

| Path | Change |
|---|---|
| `internal/obs/` | New package: OTel bootstrap, instrument definitions, zerolog↔trace hook |
| `internal/api/*.go` | Wire `otelhttp` middleware; instrument WS handshake and message paths |
| `internal/store/postgres/postgres.go` | Wrap `sql.Open` with `otelsql`; register DB stats metrics |
| `internal/store/redis/redis.go` | Add `redisotel` hook |
| `cmd/server/main.go` | `obs.Init` at startup, `obs.Shutdown` deferred |
| `deploy/compose/alloy/config.alloy` | New Alloy configuration |
| `deploy/compose/docker-compose.prod.yml` | Add `alloy` service with `mem_limit` |
| `deploy/compose/remote-deploy.sh` | Fetch the seven new SSM values into `.env` |
| `deploy/terraform/ssm.tf` | Declare the new parameters |
| `deploy/terraform/grafana.tf` | New: Grafana provider, contact point, notification policy, alert rules |
| `deploy/grafana/dashboards/*.json` | Exported dashboard artifacts, committed |

## Instrumentation

### The cardinality rule

**`game_id`, `user_id`, and client IP are span attributes and log fields only — never metric labels.**

They are unbounded. One of them on a metric label consumes the 10k free series budget and starts silently dropping data. Every instrument below obeys this rule, and a test enforces it (see Testing).

### Traces

| Layer | Mechanism | Notes |
|---|---|---|
| HTTP | `otelhttp` around the mux | Route-templated span names (`POST /games/{id}/join`), not raw URLs |
| Postgres | `XSAM/otelsql` wrapping `database/sql` | No `lib/pq` → `pgx` migration |
| Redis | `redisotel` hook | Also emits pool metrics |
| WebSocket | Custom — see below | `otelhttp` is wrong here |

**WebSocket tracing is two distinct things, not one:**

1. **Handshake span** — short-lived, covering upgrade → AUTH → accepted/rejected. This is where the real failures live: the 5-second auth timeout, origin rejection, and the per-user/per-IP connection caps.
2. **Span per inbound message** — a new root trace per message, named e.g. `ws.message MOVE`, carrying `game_id` / `user_id` / `conn_id` as attributes. Downstream Redis and Postgres spans nest naturally.

**Sampling:** parent-based `traceidratio`, 100% initially, tunable by env without a rebuild. The plausible route to the 50 GB ceiling is a runaway WS message loop, so trace volume gets checked after the first week.

### Metrics

**Automatic:** `otelhttp` request duration and count by route + status; Go runtime metrics (goroutines, heap, GC pause) — non-negotiable on a 2 GB box; `database/sql` pool stats including **waiting** connections; Redis pool hits/misses/timeouts.

**Custom** — chosen so the safeguards already in `cmd/server/main.go` become observable:

| Instrument | Labels | Rationale |
|---|---|---|
| `mighty.ws.handshake` (counter) | `outcome`: `ok`, `auth_timeout`, `auth_failed`, `origin_rejected`, `conn_limit_user`, `conn_limit_ip` | Mirrors the four tunables in `main.go`. A spike in `origin_rejected` is the `ALLOWED_ORIGINS` deploy mistake the startup log already warns about — caught in prod, where nobody reads startup logs. |
| `mighty.ws.connections.active` (gauge) | `kind`: `game`, `lobby` | Capacity truth; also reveals leaked connections (rises, never falls) |
| `mighty.ws.messages` (counter) | `type`, `outcome`: `accepted`, `rate_limited`, `invalid` | Is the 10/s + burst-20 limiter clamping real players? |
| `mighty.ratelimit.rejections` (counter) | `bucket` (e.g. `creategame`) | Same question for the HTTP limiter |
| `mighty.games.active` (gauge) | — | Product health |
| `mighty.games.created` (counter) | — | |
| `mighty.games.completed` (counter) | `outcome` | With `games.active`, exposes the created-but-never-completed leak |
| `mighty.moves` (counter) + duration histogram | `outcome`: `valid`, `invalid` | Engine correctness — an invalid-move spike means a client/server rules mismatch |
| `mighty.optimistic_conflicts` (counter) | — | State is version-tracked; conflict rate directly measures whether that's under strain |

Estimated total series including host, Postgres, Redis, and container exporters: **~3–4k of 10k** — comfortable, but only because of the cardinality rule.

### Logs

zerolog already emits structured JSON with a `component` field. No logging rewrite. Two changes:

1. A zerolog hook injects `trace_id` / `span_id` from context, so a log line jumps to its trace.
2. `LoggingMiddleware` currently logs **twice per request** (`"Incoming request"` plus the response line). Once `otelhttp` records duration and status as a metric, the pre-flight line is redundant volume against the 50 GB budget — collapse to one line per request. This is a targeted improvement to code the change already touches, not a refactor.

**Loki labels: `service`, `container`, `level`, `env`. Nothing else.** In particular `trace_id` must **not** be a label — it is unbounded and would explode stream cardinality. It stays a JSON field, linked to Tempo via a derived field.

## Collection on the box

### Alloy pipelines

| Source | Path | Destination |
|---|---|---|
| App traces + metrics | `otelcol.receiver.otlp` (gRPC :4317) → `batch` | `otelcol.exporter.otlphttp` → OTLP gateway |
| Host metrics | `prometheus.exporter.unix` → `scrape` | `prometheus.remote_write` |
| Postgres metrics | `prometheus.exporter.postgres` → `scrape` | `prometheus.remote_write` |
| Redis metrics | `prometheus.exporter.redis` → `scrape` | `prometheus.remote_write` |
| Container metrics | `prometheus.exporter.cadvisor` → `scrape` | `prometheus.remote_write` (**disabled initially**) |
| Container logs | `loki.source.docker` → `loki.process` (JSON parse) | `loki.write` |

All of Postgres, Redis, host, and container metrics come from Alloy's built-in exporters — one container, not four.

### Feedback loop to avoid

`loki.source.docker` discovers *all* containers, including Alloy. Alloy logging about shipping logs generates logs to ship. The config must **exclude Alloy's own container from discovery** and run Alloy at `info`, not `debug`.

### Memory mitigations, in order

1. `mem_limit: 256m` on the Alloy container — a hard ceiling, so the worst case is Alloy OOMing alone rather than taking Postgres with it.
2. **cadvisor starts disabled.** Enabled as an explicit later step, gated on measured headroom, with `enabled_metrics` restricted to `cpu`, `memory`, `diskIO`, `network`.
3. Trim the unix exporter's default collector set — the full default emits well over 1k series of mostly noise.

**Measured, not estimated.** Deploy Alloy, then read actual RSS and `MemAvailable` off the box before enabling cadvisor and before calling the work done. **If headroom is under ~200 MB, container metrics stay off and that is recorded as a known gap rather than quietly shipped.**

## Alerting

**Design constraint: traffic is almost nil.** Standard ratio rules are meaningless at 2 requests/minute — one failed health check becomes a 50% error rate and pages at 3am. Therefore ratio rules carry a minimum-volume guard, and the rules that earn their keep in the near term are the absolute resource ones.

### Critical → Discord

| Alert | Condition | Rationale |
|---|---|---|
| Telemetry blind | Host heartbeat metric absent 10m | Alloy or the box is gone. Without this, every other alert fails silently — monitoring that can't detect its own death is decorative. |
| Memory exhausted | `MemAvailable` < 150 MB, or sustained swap-in, 10m | **Most likely real incident** on a 2 GB box running Postgres. The 2 GB swap turns a crash into slow death, which is harder to notice. |
| Disk filling | Root volume < 15% free, 15m | 20 GB shared by Docker images, Postgres data, and Alloy's WAL; deploys accumulate images. |
| Service down | No app telemetry received from `mighty` for 2m *(Stage 3+)* | Caddy still answers, so the Route 53 `/healthz` probe may miss a partial failure. **Signal availability:** this is app-metric absence, so it only exists from Stage 3. Container-level crash-loop detection needs cadvisor; until then, container death is covered by the existing Route 53 probe, and that is an accepted gap for Stages 1–2. |
| 5xx errors | > 5 responses in 5m (**absolute**, not ratio) *(Stage 3+)* | Absolute precisely because the denominator is tiny. |
| Postgres saturation | Exporter: `numbackends` > 80% of `max_connections`, 5m. Refined at Stage 3 by also alerting on `sql.DBStats` waiting-for-connection > 0 for 5m. | The exporter-based rule works from Stage 2. Waiting connections — an app-side metric — are the earliest honest signal of pool pressure, so they refine the rule once app metrics exist. |

### Warning → Discord

| Alert | Condition | Rationale |
|---|---|---|
| WS handshake failures | non-`ok` outcomes > 30% for 15m, min 10 attempts in window | Catches the `ALLOWED_ORIGINS` deploy mistake in prod |
| Latency degradation | p95 request duration > 1s, 15m, min 10 requests in window | |
| Optimistic conflict spike | > 1 conflict/minute sustained 10m | Measures strain on version-tracked state. **This threshold is a guess** — there is no baseline yet. Stage 5 records it as requiring re-tuning after two weeks of real data. |
| Free-tier ceiling | Billable series > 8k, or log ingest projecting past 50 GB | Prevents silent throttling — hitting the cap means losing data exactly when it's needed |

Ten rules total, plus the three existing CloudWatch alarms retained unchanged.

**Deliberately excluded:**

- *Games created but never completed* — no baseline traffic to tune a threshold against. Revisit once there are real users.
- *TLS certificate expiry* — Caddy auto-renews, and a renewal failure surfaces through the existing Route 53 probe.

### Dashboards

Three built here, two taken for free:

1. **Service health** — request rate / errors / latency by route, WS connections, handshake outcomes
2. **Box** — CPU, memory, swap, disk, per-container
3. **Game engine** — games active/created/completed, moves, invalid moves, optimistic conflicts

Grafana Cloud ships integration dashboards for Postgres and Redis alongside those exporters. Do not rebuild what comes free.

## Local development

**The OTel SDK is a no-op when unconfigured.** With `OTEL_EXPORTER_OTLP_ENDPOINT` unset, `obs.Init` returns no-op tracer and meter providers. This is load-bearing: the dev `docker-compose.yml` has no Alloy, and `go test ./...` must not attempt network exports or slow down. **Instrumentation is opt-in by env, off by default.**

An `observability` compose profile optionally runs Alloy locally, shipping to the same Grafana Cloud stack tagged `env=dev` — cheaper than standing up local Tempo + Loki + Grafana, and dashboards filter on the label.

## Testing

The instrumentation is testable behavior, not just plumbing.

| Test | Mechanism |
|---|---|
| Span creation per WS handshake outcome | In-memory `tracetest.SpanRecorder`; extends the existing `ws_hardening_test.go`, which already exercises auth timeout, origin rejection, and both connection caps |
| `mighty.ws.handshake` increments with correct `outcome` label | `sdkmetric` manual reader |
| **Cardinality guard** | Test fails if any instrument declares `game_id`, `user_id`, or an IP as an attribute key. Cheap, and it guards the one mistake that silently breaks the free tier — worth a test, not a comment. |
| No-op when disabled | With env unset, `obs.Init` exports nothing and the existing godog E2E suite passes unchanged |
| No goroutine leaks on shutdown | `goleak` (already in `go.mod`) around `obs.Shutdown` |

### On-box verification

Usually skipped; explicitly required here:

1. Confirm data lands in all three backends (Prometheus, Loki, Tempo).
2. Read actual Alloy RSS and `MemAvailable` on the box.
3. **Deliberately trip one alert rule** and confirm Discord receives it. An alert path that has never fired is an assumption, not a safety net.

## Rollout

Five stages, each independently shippable and reversible.

| Stage | Content | Go code risk |
|---|---|---|
| 1 | Alloy + host metrics + container logs + Discord contact point + Box dashboard. Alerts: telemetry-blind, memory exhausted, disk filling | none |
| 2 | Postgres + Redis exporters, their free dashboards. Alerts: Postgres saturation (exporter-based) | none |
| 3 | `obs` package, `otelhttp`, custom instruments, service + engine dashboards. Alerts: service down, 5xx, WS handshake failures, latency, optimistic conflicts, Postgres `DBStats` refinement | yes |
| 4 | Traces: `otelsql`, `redisotel`, WS spans, log↔trace linking | yes |
| 5 | Measure headroom → cadvisor decision (and crash-loop alert if enabled); free-tier ceiling alert; re-tune the optimistic-conflict threshold against real data; record remaining gaps | none |

Stages 1–2 touch no Go code and already close the biggest real gap: you would know about memory pressure and disk fill, which is what will actually bite a 2 GB box. The riskiest changes land only after the pipeline has proven itself.

## Out of Scope

- Frontend telemetry (web, Electron, Swift) — different problem shape; Sentry is the better fit
- Instance resize — blocked by the $30/mo budget alarm
- Profiling (Grafana Pyroscope) — free tier includes it, but no current need
- Synthetic monitoring beyond the existing Route 53 probe
- Replacing or modifying any existing CloudWatch alarm
