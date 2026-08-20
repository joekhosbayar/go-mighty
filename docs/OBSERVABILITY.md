# Observability — Metrics Reference & Operational Learnings

This document is the source of truth for the metrics Mighty actually exports, the
Grafana dashboard that visualizes them, and the hard-won lessons about OTel
semantic conventions that determine what is (and is not) observable.

It exists because of a concrete failure: the first cut of the data-plane
dashboard used **guessed** metric names (`redis_commands_total`,
`db_sql_client_requests_total`, …) that do not exist, so every panel rendered
"no data." The names below were verified against the actual library source in
the Go module cache, not assumed. Keep this file in sync with instrumentation
changes (see the Docs-with-Code rule in `AGENTS.md`).

---

## 1. Architecture at a glance

```
mighty server (OTel SDK)
   │  OTLP gRPC :4317
   ▼
Grafana Alloy  ──► Prometheus remote_write ──► Grafana Cloud (Mimir)
   │           ──► Loki (docker logs)
   │           ──► host metrics (prometheus.exporter.unix, job="mighty/host")
   ▼
OTLP traces ──► Tempo
```

- Metrics pipeline config: `deploy/compose/alloy/config.alloy`
- Alert rules (Terraform): `deploy/terraform/grafana.tf`
- Custom app instruments: `internal/obs/metrics.go`
- Data-plane instrumentation: `internal/store/postgres/postgres.go`,
  `internal/store/redis/redis.go`, `cmd/server/main.go`

Library versions that define the metric names below (from `go.mod`):

| Library | Version | Role |
|---|---|---|
| `go.opentelemetry.io/otel/sdk` | v1.44.0 | OTel SDK / meter provider |
| `github.com/XSAM/otelsql` | v0.43.0 | Postgres (`database/sql`) instrumentation |
| `github.com/redis/go-redis/extra/redisotel/v9` | v9.21.0 | Redis pool + command instrumentation |

> **Naming rule:** instruments are registered with OTel dot-names
> (`db.client.connections.usage`). The Prometheus exporter converts these to
> snake_case (`db_client_connections_usage`) and appends `_bucket`/`_sum`/`_count`
> to histograms and `_total` to monotonic counters. Always query the
> **Prometheus** form in Grafana.

---

## 2. Cardinality rule (do not violate)

`game_id`, `user_id`, `conn_id`, and client IPs are **span attributes and log
fields only — never metric labels, never Loki labels, never span names.**

Violating this risks exhausting the Grafana Cloud series cap (~10,000 series).
This is why there is intentionally **no per-socket frame metric** and no
per-command Redis label (see §5).

---

## 3. Application-layer metrics (custom, `internal/obs/metrics.go`)

All use the `mighty.` namespace → Prometheus `mighty_*`.

| Prometheus metric | Type | Labels | Notes |
|---|---|---|---|
| `mighty_ws_handshake_total` | counter | `kind`, `outcome` | WS upgrade attempts |
| `mighty_ws_connections_active` | up/down gauge | `kind` | `kind` = `game` / `lobby` |
| `mighty_ws_messages_total` | counter | `type`, `outcome` | inbound frames; `type` is sanitized |
| `mighty_ratelimit_rejections_total` | counter | `bucket` | |
| `mighty_games_created_total` | counter | — | |
| `mighty_moves_total` | counter | `outcome` | |
| `mighty_move_duration_seconds` | histogram | — | move processing latency |
| `mighty_optimistic_conflicts_total` | counter | — | optimistic-concurrency retries |

Standard OTel HTTP server metrics are also present
(`http_server_request_duration_seconds_{bucket,count}` with `http_route`,
`http_response_status_code`).

---

## 4. Data-plane metrics (verified against library source)

### 4.1 Postgres — otelsql v0.43.0

Registered via `otelsql.Open(...)` + `otelsql.RegisterDBStatsMetrics(...)` in
`internal/store/postgres/postgres.go`. Pool-stats instruments use the
`db.sql.connection` namespace.

| Prometheus metric | Type | Meaning |
|---|---|---|
| `db_sql_connection_open` | gauge | established connections (in-use + idle) |
| `db_sql_connection_max_open` | gauge | `SetMaxOpenConns` limit |
| `db_sql_connection_wait` | counter | total times a goroutine waited for a conn |
| `db_sql_connection_wait_duration` | counter (ms) | total time blocked waiting |
| `db_sql_connection_closed_max_idle` | counter | closed due to `SetMaxIdleConns` |
| `db_sql_connection_closed_max_idle_time` | counter | closed due to `SetConnMaxIdleTime` |
| `db_sql_connection_closed_max_lifetime` | counter | closed due to `SetConnMaxLifetime` |

**Query latency / rate** come from a single histogram (OTel semconv
`db.client.operation.duration`, via `dbconv.NewClientOperationDuration`):

| Prometheus metric | Labels |
|---|---|
| `db_client_operation_duration_{bucket,sum,count}` | `db_system_name="postgresql"`, `db_operation_name` |

`db_operation_name` values (from `methods.go`): `sql.conn.query`,
`sql.conn.exec`, `sql.conn.prepare`, `sql.conn.ping`, `sql.conn.begin_tx`,
`sql.tx.commit`, `sql.tx.rollback`, `sql.stmt.exec`, `sql.stmt.query`,
`sql.connector.connect`.

> ⚠️ There is **no separate "idle" or "waiting" gauge**. `open` is the total;
> pool pressure shows up as the **rate of `db_sql_connection_wait`** and rising
> `wait_duration`. There is also **no** `db_sql_client_requests_total` — request
> rate is `rate(db_client_operation_duration_count[...])`.

### 4.2 Redis — redisotel v9.21.0

Registered via `redisotel.InstrumentMetrics(...)` in
`internal/store/redis/redis.go` (pool `game`) and `cmd/server/main.go` (pool
`ratelimit`, via `redisotel.WithPoolName("ratelimit")`).

**Connection-pool metrics** (all carry `pool_name`; `usage` also carries
`state`):

| Prometheus metric | Type | Labels | Meaning |
|---|---|---|---|
| `db_client_connections_usage` | up/down | `pool_name`, `state=idle\|used` | live conns by state |
| `db_client_connections_max` | gauge | `pool_name` | `PoolSize` |
| `db_client_connections_idle_max` / `idle_min` | gauge | `pool_name` | config limits |
| `db_client_connections_waits` | counter | `pool_name` | times a conn was waited for |
| `db_client_connections_waits_duration` | counter (ns) | `pool_name` | total wait time |
| `db_client_connections_timeouts` | counter | `pool_name` | pool-wait timeouts |
| `db_client_connections_hits` / `misses` | counter | `pool_name` | free-conn found / not found |
| `db_client_connections_create_time` | histogram (ms) | `pool_name`, `status`, `error_type` | dial latency |
| `db_client_connections_use_time` | histogram (ms) | `pool_name`, `type`, `status`, `error_type` | **per-command latency** |

**Per-command metric** — `db_client_connections_use_time` is the only
command-level metric. Its labels:

- `pool_name` — `game` or `ratelimit`
- `type` — `command` or `pipeline`
- `status` — `ok`, `error`, or `nil` (redis.Nil / key-miss)
- `error_type` — `none`, `context_canceled`, `context_timeout`, `other`

> ⚠️ **Units are milliseconds**, not seconds. Set the Grafana panel unit to
> `ms` when charting `db_client_connections_use_time_bucket`.

---

## 5. Known observability limitations (by design / by library)

These are the "you can't have that metric" findings, with the reason.

1. **No per-command Redis metric.** redisotel does not tag individual command
   names (GET/SET/SETNX). Command identity exists only as **trace span names**,
   not metric labels — adding it as a label would also blow the cardinality
   budget. Consequence: a "lock acquisition rate" panel filtered on
   `command=~"SETNX|SET"` is impossible. Use pool **waits/timeouts** as the
   contention signal instead, and use **traces** for per-command breakdowns.

2. **No per-socket / per-connection frame metrics.** `conn_id` is a span
   attribute only (cardinality rule, §2). Frames-per-socket must be answered
   from traces, not metrics.

3. **No concurrent-unique-players gauge.** `mighty_ws_connections_active` counts
   connections, not distinct users (a user can hold game + lobby sockets). A
   true unique-player gauge would need a `user_id` label, which is disallowed.

4. **Game status is invisible.** `games.status` in Postgres is frozen because
   `UpdateGameStatus` (defined in `internal/store/postgres/postgres.go`) is
   **never called** — it's dead code. `ListGamesByStatus` merges Postgres IDs
   with live Redis state, so the DB column is not the source of truth. There is
   no games-by-phase metric. This is a product/observability gap, not a naming
   issue.

5. **No outbound WS frame metric.** `mighty_ws_messages_total` counts inbound
   frames only.

6. **No container (cAdvisor) metrics.** Alloy ships host metrics
   (`job="mighty/host"`) but not per-container CPU/mem.

---

## 6. Grafana dashboard

**Mighty - Service Health** — uid `agwm6b`, folder `Mighty`
(`https://<your-stack>/d/agwm6b/mighty-service-health`).

Panels (app layer): Active WS Connections, HTTP Request Rate, HTTP Error Rate
(4xx/5xx), p95 Request Latency, WS Handshake Outcomes, WS Message Rate by Type,
Games Created Rate, Move Outcomes, Move Latency p95/p99, Rate Limit Rejections,
Optimistic Conflicts.

**DATA PLANE** row (queries verified against §4):

| Panel | Query |
|---|---|
| Postgres Connections | `db_sql_connection_open`, `db_sql_connection_max_open`, `rate(db_sql_connection_wait[5m])` |
| Postgres Query Rate | `sum by (db_operation_name) (rate(db_client_operation_duration_count{db_system_name="postgresql"}[5m]))` |
| Postgres Query Latency p95 | `histogram_quantile(0.95, sum by (le, db_operation_name) (rate(db_client_operation_duration_bucket{db_system_name="postgresql"}[5m])))` |
| Redis Connections by Pool | `sum by (pool_name, state) (db_client_connections_usage)` |
| Redis Command Rate | `sum by (pool_name, type) (rate(db_client_connections_use_time_count[5m]))` |
| Redis Command Latency p95 | `histogram_quantile(0.95, sum by (le, pool_name) (rate(db_client_connections_use_time_bucket[5m])))` (unit `ms`) |
| Redis Errors | `sum by (error_type) (rate(db_client_connections_use_time_count{status="error"}[5m]))` |
| Redis Pool Waits & Timeouts | `sum by (pool_name) (rate(db_client_connections_waits[5m]))`, `..._timeouts[5m])` |

Datasources: Prometheus/Mimir `grafanacloud-prom`, Loki `grafanacloud-logs`,
Tempo `grafanacloud-traces`.

---

## 7. Operational learnings (the "how we got burned" section)

1. **Verify metric names against library source, never guess.** The data-plane
   dashboard initially used invented names and rendered empty. The fix came from
   reading `instruments.go` / `metrics.go` in the module cache
   (`~/go/pkg/mod/...`). When a panel shows "no data," diff the query against
   the instrument's registered name first — before touching the collector.

2. **OTel dot-name → Prometheus snake_case.** `db.client.connections.usage` →
   `db_client_connections_usage`; histograms gain `_bucket/_sum/_count`;
   monotonic counters gain `_total`. Observable UpDownCounters and gauges keep
   the bare name.

3. **Attribute names get sanitized too.** `pool.name` → `pool_name`,
   `db.system.name` → `db_system_name`, `db.operation.name` →
   `db_operation_name`. A `{pool="game"}` matcher silently matches nothing — the
   label is `pool_name`.

4. **The label you want may not exist for a good reason.** Per-command and
   per-connection labels are omitted deliberately (cardinality). Reach for
   traces when you need high-cardinality dimensions.

5. **Watch units.** Redis command latency is **ms**; otelsql operation duration
   is **seconds**. A panel with the wrong unit looks broken even when data
   flows.

6. **A working app-layer pipeline proves the collector is fine.** If `mighty_*`
   panels have data but `db_*` panels don't, the problem is the query/metric
   name, not Alloy or remote_write.

---

## 8. Outstanding work (not yet done)

- [ ] Codify the dashboard JSON into `deploy/grafana/dashboards/` (observability
      plan Task 10 Step 2) so it's version-controlled and reproducible.
- [ ] Add app-layer alert rules to `deploy/terraform/grafana.tf`:
      `service_down`, `http_5xx`, `ws_handshake_failures`, `latency_degraded`,
      `optimistic_conflicts` (plan Task 10 Steps 3–4). Only 3 critical
      host-level rules exist today.
- [ ] Fix the game-status gap: wire up `UpdateGameStatus` (or add a
      games-by-phase gauge from Redis) so lifecycle state is observable.
- [ ] Add container metrics (cAdvisor) to Alloy if per-container visibility is
      needed.
