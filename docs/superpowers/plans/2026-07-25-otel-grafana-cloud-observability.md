# OpenTelemetry + Grafana Cloud Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship metrics, logs, and traces from the go-mighty production stack to Grafana Cloud's free tier via a single Grafana Alloy container, with ten alert rules routed to Discord.

**Architecture:** One `alloy` container is the sole egress path and the only holder of Grafana Cloud credentials. The Go app exports OTLP to `alloy:4317` over the compose network and never learns the vendor exists. Alloy additionally scrapes its own built-in host/Postgres/Redis exporters and tails container logs off the Docker socket. Alert rules and the Discord contact point are managed in OpenTofu; dashboards are built in the UI and their JSON committed as artifacts.

**Tech Stack:** Go 1.25, OpenTelemetry Go SDK, `otelhttp`, `XSAM/otelsql`, `redisotel`, zerolog, Grafana Alloy v1.10, Grafana Cloud (Prometheus/Loki/Tempo), OpenTofu + `grafana/grafana` provider, Docker Compose, AWS SSM Parameter Store.

**Spec:** `docs/superpowers/specs/2026-07-25-otel-grafana-cloud-observability-design.md`

---

## Global Constraints

- **Instance is `t4g.small` (2 vCPU / 2 GB RAM + 2 GB swap). No resize.** A `t4g.medium` adds ~$12/mo against a budget alarm set at $25 forecast / $30 actual.
- **Alloy container is hard-capped at `mem_limit: 256m`.** Worst case Alloy OOMs alone rather than taking Postgres with it.
- **Free-tier ceilings: 10k metric series, 50 GB logs, 50 GB traces, 14-day retention.**
- **Cardinality rule, absolute:** `game_id`, `user_id`, and client IP are span attributes and log fields **only — never metric labels, never Loki labels.** Task 7 adds a test that enforces this.
- **`trace_id` is never a Loki label.** It stays a JSON field, linked to Tempo via a derived field.
- **Scrape interval is 60s everywhere.** Not the 15s default.
- **The OTel SDK must be a no-op when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset.** Dev compose has no Alloy and `go test ./...` must not attempt network exports. Instrumentation is opt-in by env, off by default.
- **cadvisor starts disabled.** Enabled only in Task 12, gated on measured headroom.
- **Existing CloudWatch alarms in `deploy/terraform/alarms.tf` are not modified or removed.** They are out-of-band and work precisely when the in-band path is dead.
- **Existing test suites must keep passing unchanged** except where a task explicitly updates a test.
- Existing lint config is `.golangci.yml`; run `golangci-lint run` before each commit.
- Deploy is manual: `docker buildx build --platform linux/arm64 --push` then `deploy/scripts/deploy.sh`. Production is **arm64** — always pass `--platform linux/arm64`.

## Deviations from the Spec

Three, all discovered while reading the code to write this plan.

1. **`mighty.games.active` and `mighty.games.completed` are dropped.** `postgres.Store.UpdateGameStatus` (`internal/store/postgres/postgres.go:89`) is dead code — never called from any non-test file. So `games.status` is frozen at its creation value and there is no trustworthy source for active or completed game counts. Building these metrics anyway would produce a dashboard that lies. `mighty.games.created` is kept (reliable — recorded at creation). Recorded as a known gap in Task 12.
2. **Two outcome values added beyond the spec's list.** `mighty.ws.handshake` gains `auth_unavailable` (a Cognito/JWKS outage) and `upgrade_failed`. Conflating a dependency outage with bad credentials is a real diagnostic loss, and both are bounded values.
3. **WebSocket routes are excluded from `otelhttp` entirely.** `otelhttp`'s response-writer wrapper does not reliably implement `http.Hijacker`, which `gorilla/websocket` requires for the upgrade. Wrapping WS routes risks breaking every WebSocket. Task 9 adds a regression test asserting the upgrade still succeeds through the full middleware chain.

## Corrections Applied During Execution

Seven defects were found in this plan while executing it, plus one found while
answering a question afterwards. All are corrected in the text above. Recorded
here because the pattern is instructive: **every one was in a passage written
from memory of an API or a file, rather than transcribed from the actual
source.** Passages copied from files that had been read were clean throughout.

| # | Defect | Consequence had it shipped |
|---|---|---|
| 1 | `obs.Log` specified to return `zerolog.Logger` by value | Chained `obs.Log(ctx).Warn()` cannot compile — zerolog's methods have pointer receivers |
| 2 | `LoggingMiddleware` rewrite renamed `responseCode`→`status` and dropped the 3xx/1xx switch cases | Broken log queries; two status classes silently unlogged |
| 3 | Handshake metric test had `kind` and `outcome` co-varying | Test passed against an implementation that dropped the `kind` attribute entirely |
| 4 | `inMsg.Type` — raw client JSON — passed as a metric label | **Any authenticated client could exhaust the 10k free-tier series budget, after which Grafana silently drops data** |
| 5 | Double-count guard used `websocket.ErrBadHandshake` | Client-side sentinel; server `Upgrade` never returns it, so the guard was dead code and every origin rejection counted twice — inflating the metric behind the handshake-failure alert |
| 6 | `otelhttp.WithRouteTag` | Does not exist in otelhttp v0.69.0; `http.route` is derived from `http.Request.Pattern` automatically |
| 7 | Task 11 Step 6 assumed origin rejection was undetectable post-upgrade | Stale after defect #5's fix; would have made the span and the metric disagree about why a handshake failed |
| 8 | Task 1 Step 5 declared Grafana params as Terraform `data` sources | **Would have written the Grafana access token in plaintext into local unencrypted `terraform.tfstate`, for no consumer** |
| 9 | `telemetry_blind` used bare `absent()` | `absent()` returns an empty vector when data exists → No Data → **Alerting**. The rule meant to detect a dead collector would have fired permanently in both states |
| 10 | `disk_filling` filtered `mountpoint="/rootfs"` | node_exporter strips `rootfs_path` from the label; real value is `/`. Matched zero series → permanent No Data → **permanent alerting** |

Defects 4, 5 and 8 would have reached production silently. Defects 9 and 10
would have been loudly wrong instead — two of the three launch alerts firing
into Discord on install and never clearing, which is the failure mode most
likely to get a monitoring channel muted for good.

None was caught by a test. The questions that did catch them were: "where does
this value come from?" (4), "who actually reads this?" (8), and "can this
expression return zero series?" (9, 10).

## File Structure

| Path | Responsibility |
|---|---|
| `internal/obs/obs.go` | OTel SDK lifecycle: `Config`, `Init`, `Provider.Shutdown`. No-op when unconfigured. |
| `internal/obs/obs_test.go` | Lifecycle tests: disabled path, enabled path, no goroutine leaks. |
| `internal/obs/metrics.go` | All metric instrument definitions + nil-safe recording methods. The only file allowed to name metric attributes. |
| `internal/obs/metrics_test.go` | Instrument behavior + the cardinality guard test. |
| `internal/obs/log.go` | `Log(ctx)` — zerolog logger with `trace_id`/`span_id` bound from the span in ctx. |
| `internal/obs/log_test.go` | Trace-field injection present with a span, absent without. |
| `internal/obs/trace.go` | `Tracer()` accessor and WS span helpers. |
| `internal/api/options.go` | +`WithMetrics` option. |
| `internal/api/ws.go`, `lobby_ws.go` | Handshake + message instrumentation; per-message spans. |
| `internal/api/handler.go` | `LoggingMiddleware` collapsed to one line/request, trace-aware. |
| `internal/api/ratelimit_middleware.go` | Rate-limit rejection counter. |
| `internal/service/game_service.go` | Move, conflict, and game-created metrics. |
| `internal/store/postgres/postgres.go` | `otelsql` wrapping + DB stats metrics. |
| `internal/store/redis/redis.go` | `redisotel` tracing + metrics hooks. |
| `cmd/server/main.go` | `obs.Init`, shutdown, `otelhttp` wiring, route tags. |
| `deploy/compose/alloy/config.alloy` | Alloy pipelines. |
| `deploy/compose/docker-compose.prod.yml` | `alloy` service. |
| `docker-compose.yml` | Opt-in `observability` profile for local dev. |
| `deploy/compose/remote-deploy.sh` | Fetch new SSM params into `.env`. |
| `deploy/terraform/ssm.tf` | **Unchanged** — the Grafana parameters are deliberately NOT declared in Terraform (see Task 1 Step 5). |
| `deploy/terraform/grafana.tf` | Provider, folder, contact point, notification policy, alert rules. |
| `deploy/terraform/variables.tf` | New Grafana/Discord variables. |
| `deploy/grafana/dashboards/*.json` | Exported dashboard artifacts. |
| `docs/OBSERVABILITY.md` | Runbook: what's collected, how to query, known gaps. |

---

## Stage 1 — Pipeline proof (no Go changes)

### Task 1: Grafana Cloud account, SSM parameters, and Terraform variables

Nothing downstream can be verified without real credentials. This task ends with secrets in SSM and OpenTofu able to reference them.

**Files:**
- Modify: `deploy/terraform/variables.tf`
- Modify: `deploy/terraform/terraform.tfvars.example`
- **Not** `deploy/terraform/ssm.tf` — see Step 5.

**Interfaces:**
- Produces: SSM parameters `/mighty/grafana/{otlp_endpoint,otlp_instance_id,prom_url,prom_user_id,loki_url,loki_user_id,token}`; OpenTofu variables `grafana_url`, `grafana_sa_token`, `discord_webhook_url`, `prom_datasource_uid`.

- [ ] **Step 1: Create the Grafana Cloud stack (manual, in a browser)**

Sign up at https://grafana.com/auth/sign-up/create-user and create a stack. Then from the stack's home page collect, for each of Prometheus, Loki, and Tempo, the **remote-write/push URL** and the **User/Instance ID** — each backend has its own numeric ID; they are not the same value.

Then create one access-policy token: **Administration → Users and access → Access Policies → Create access policy**, scopes `metrics:write`, `logs:write`, `traces:write`. Create a token under it and copy it once — it is not shown again.

Finally, from **Connections → Data sources**, open the `grafanacloud-<stack>-prom` datasource and copy its **UID** from the browser URL (`/datasources/edit/<UID>`).

- [ ] **Step 2: Create the Discord webhook (manual)**

In your Discord server: **Server Settings → Integrations → Webhooks → New Webhook**, pick a channel, **Copy Webhook URL**.

- [ ] **Step 3: Write the SSM parameters**

Replace each `<...>` with the real value. The `token` is `SecureString`; the rest are plain because URLs and numeric IDs are not secrets.

```bash
REGION=us-east-1

for pair in \
  "otlp_endpoint=<https://otlp-gateway-prod-us-east-0.grafana.net/otlp>" \
  "otlp_instance_id=<numeric stack id>" \
  "prom_url=<https://prometheus-prod-XX.grafana.net/api/prom/push>" \
  "prom_user_id=<numeric prom id>" \
  "loki_url=<https://logs-prod-XX.grafana.net/loki/api/v1/push>" \
  "loki_user_id=<numeric loki id>" ; do
  name="${pair%%=*}"; value="${pair#*=}"
  aws ssm put-parameter --region "$REGION" \
    --name "/mighty/grafana/$name" --type String --value "$value" --overwrite
done

aws ssm put-parameter --region "$REGION" \
  --name /mighty/grafana/token --type SecureString \
  --value '<access policy token>' --overwrite
```

- [ ] **Step 4: Verify the parameters read back**

Run:
```bash
aws ssm get-parameters-by-path --region us-east-1 \
  --path /mighty/grafana --with-decryption \
  --query 'Parameters[].Name' --output text
```
Expected: all seven names listed.

- [ ] **Step 5: Do NOT declare the Grafana parameters in OpenTofu**

**Deliberately empty step. Add nothing to `deploy/terraform/ssm.tf`.**

An earlier draft of this plan told you to append a `data "aws_ssm_parameter" "grafana"` block here, with a comment claiming that using a `data` source rather than a `resource` keeps the access token out of state. **That reasoning is wrong and the block is a credential leak.**

Terraform stores the results of **data sources** in state, in plaintext, exactly as it stores managed resources. `deploy/terraform/terraform.tfstate` in this project is local and unencrypted. So that block would have written your Grafana access-policy token — the one with `metrics:write, logs:write, traces:write` — into a plaintext file on the operator's laptop, in service of nothing.

In service of nothing, precisely: **no Terraform resource in this plan ever references those data sources.** They were purely declarative. The only consumer of these parameters is `remote-deploy.sh`, which reads them straight from SSM on the box at deploy time (Task 2, Step 3) using the instance role. Terraform has no reason to see them at all.

The general rule this violated, worth remembering beyond this plan: *a secret should be read by exactly the thing that uses it, as late as possible.* Routing it through a tool that only passes it along adds a copy at rest and buys nothing.

If you have already applied a version of this block, the token is in your state file. Remove the block, then either rotate the Grafana token or scrub the state (`tofu state rm` does not remove historical copies — check `terraform.tfstate.backup` too).

- [ ] **Step 6: Add the OpenTofu variables**

Append to `deploy/terraform/variables.tf`:

```hcl
variable "grafana_url" {
  type        = string
  description = "Grafana Cloud stack URL, e.g. https://mighty.grafana.net"
}

variable "grafana_sa_token" {
  type        = string
  description = "Grafana service account token with Admin on the stack (for managing alert rules)"
  sensitive   = true
}

variable "discord_webhook_url" {
  type        = string
  description = "Discord webhook URL that alert notifications are posted to"
  sensitive   = true
}

variable "prom_datasource_uid" {
  type        = string
  description = "UID of the grafanacloud-<stack>-prom datasource that alert rules query"
}
```

Append the same four keys with placeholder values to `deploy/terraform/terraform.tfvars.example`, then set the real values in `terraform.tfvars` (already gitignored).

- [ ] **Step 7: Create the Grafana service account token (manual)**

`grafana_sa_token` is **not** the access-policy token from Step 1 — that one only writes telemetry. In the stack: **Administration → Users and access → Service accounts → Add service account**, role `Admin`, then **Add service account token**.

- [ ] **Step 8: Verify OpenTofu still plans cleanly**

Run: `cd deploy/terraform && tofu init && tofu plan`
Expected: plan succeeds and proposes **no changes** to existing resources. If it proposes changes to `aws_instance.api` or any alarm, stop — something else drifted and must be resolved before continuing.

- [ ] **Step 9: Commit**

```bash
git add deploy/terraform/variables.tf deploy/terraform/terraform.tfvars.example
git commit -m "feat(obs): declare Grafana Cloud SSM parameters and OpenTofu variables"
```

---

### Task 2: Alloy container shipping host metrics and container logs

**Files:**
- Create: `deploy/compose/alloy/config.alloy`
- Modify: `deploy/compose/docker-compose.prod.yml`
- Modify: `deploy/compose/remote-deploy.sh`

**Interfaces:**
- Consumes: SSM parameters from Task 1.
- Produces: an OTLP gRPC receiver reachable at `alloy:4317` on the compose network (used from Task 10); host metrics under `job="mighty/host"`; container logs with labels `service`, `container`, `level`, `env`.

- [ ] **Step 1: Write the Alloy config**

Create `deploy/compose/alloy/config.alloy`:

```alloy
// Grafana Alloy: the single telemetry egress path for the Mighty stack.
// Everything here is deliberately 60s-interval and label-frugal — the free
// tier allows 10k series and this box has 2GB of RAM.

logging {
  level  = "info"
  format = "logfmt"
}

// ---------------------------------------------------------------- destinations

prometheus.remote_write "grafana_cloud" {
  endpoint {
    url = sys.env("GRAFANA_PROM_URL")

    basic_auth {
      username = sys.env("GRAFANA_PROM_USER_ID")
      password = sys.env("GRAFANA_TOKEN")
    }

    // Small shard/queue budget: this box has one service, not a fleet.
    queue_config {
      capacity             = 2500
      max_shards           = 2
      max_samples_per_send = 500
    }
  }

  // Bound the WAL. An unbounded WAL during a multi-day Grafana Cloud outage
  // would fill the 20GB root volume and take the game down with it —
  // observability must not become the outage.
  wal {
    truncate_frequency  = "30m"
    max_keep_alive_time = "2h"
  }
}

loki.write "grafana_cloud" {
  endpoint {
    url = sys.env("GRAFANA_LOKI_URL")

    basic_auth {
      username = sys.env("GRAFANA_LOKI_USER_ID")
      password = sys.env("GRAFANA_TOKEN")
    }
  }
}

otelcol.auth.basic "grafana_cloud" {
  username = sys.env("GRAFANA_OTLP_INSTANCE_ID")
  password = sys.env("GRAFANA_TOKEN")
}

otelcol.exporter.otlphttp "grafana_cloud" {
  client {
    endpoint = sys.env("GRAFANA_OTLP_ENDPOINT")
    auth     = otelcol.auth.basic.grafana_cloud.handler
  }

  sending_queue {
    enabled    = true
    queue_size = 1000
  }
}

// ------------------------------------------------------------------ app (OTLP)
// Binds 0.0.0.0 inside the container only; the compose file deliberately does
// not publish 4317 to the host, so this is reachable from `mighty` and nothing
// else.

otelcol.receiver.otlp "app" {
  grpc {
    endpoint = "0.0.0.0:4317"
  }

  output {
    traces  = [otelcol.processor.batch.default.input]
    metrics = [otelcol.processor.batch.default.input]
  }
}

otelcol.processor.batch "default" {
  send_batch_size = 512
  timeout         = "10s"

  output {
    traces  = [otelcol.exporter.otlphttp.grafana_cloud.input]
    metrics = [otelcol.exporter.otlphttp.grafana_cloud.input]
  }
}

// ----------------------------------------------------------------- host metrics
// set_collectors REPLACES the default set. The full default emits well over
// 1k series of mostly noise against a 10k budget.

prometheus.exporter.unix "host" {
  // Alloy runs in a container; without these it would report the container's
  // view of memory and disk rather than the box's.
  procfs_path = "/host/proc"
  sysfs_path  = "/host/sys"
  rootfs_path = "/rootfs"

  set_collectors = [
    "cpu",
    "diskstats",
    "filesystem",
    "loadavg",
    "meminfo",
    "netdev",
    "stat",
    "vmstat",
  ]

  filesystem {
    mount_points_exclude = "^/(dev|proc|sys|run|host|rootfs/(dev|proc|sys|run))($|/)"
    fs_types_exclude     = "^(autofs|binfmt_misc|cgroup2?|configfs|debugfs|devpts|devtmpfs|fusectl|hugetlbfs|mqueue|nsfs|overlay|proc|pstore|securityfs|selinuxfs|squashfs|sysfs|tracefs)$"
  }
}

discovery.relabel "host" {
  targets = prometheus.exporter.unix.host.targets

  rule {
    target_label = "job"
    replacement  = "mighty/host"
  }

  rule {
    target_label = "instance"
    replacement  = "mighty-api"
  }

  rule {
    target_label = "env"
    replacement  = sys.env("MIGHTY_ENV")
  }
}

prometheus.scrape "host" {
  targets         = discovery.relabel.host.output
  forward_to      = [prometheus.remote_write.grafana_cloud.receiver]
  scrape_interval = "60s"
}

// ---------------------------------------------------------------- container logs

discovery.docker "containers" {
  host             = "unix:///var/run/docker.sock"
  refresh_interval = "30s"
}

discovery.relabel "container_logs" {
  targets = discovery.docker.containers.targets

  // Drop Alloy's own container. Alloy logging about shipping logs generates
  // logs to ship — a feedback loop that bills you for its own noise.
  rule {
    source_labels = ["__meta_docker_container_name"]
    regex         = "/?.*alloy.*"
    action        = "drop"
  }

  rule {
    source_labels = ["__meta_docker_container_name"]
    regex         = "/?(.*)"
    target_label  = "container"
  }

  rule {
    target_label = "service"
    replacement  = "mighty"
  }

  rule {
    target_label = "env"
    replacement  = sys.env("MIGHTY_ENV")
  }
}

loki.source.docker "containers" {
  host             = "unix:///var/run/docker.sock"
  targets          = discovery.relabel.container_logs.output
  forward_to       = [loki.process.zerolog.receiver]
  refresh_interval = "30s"
}

loki.process "zerolog" {
  // Promote `level` only. trace_id stays a FIELD, never a label: it is
  // unbounded and one label per trace would explode stream cardinality.
  // Non-JSON producers (postgres, redis) simply yield an empty level here,
  // which is acceptable — they are low-volume and queried by container.
  stage.json {
    expressions = { level = "level" }
  }

  stage.labels {
    values = { level = "level" }
  }

  // Alloy attaches the discovery filename; it is per-container-id and would
  // create a new stream on every container recreate.
  stage.label_drop {
    values = ["filename"]
  }

  forward_to = [loki.write.grafana_cloud.receiver]
}
```

- [ ] **Step 2: Add the Alloy service to the prod compose file**

Add to `deploy/compose/docker-compose.prod.yml` under `services:`, and add `alloy_data:` to the `volumes:` block at the bottom.

```yaml
  # Single telemetry egress path. The only container holding Grafana Cloud
  # credentials; `mighty` exports to alloy:4317 and never learns the vendor
  # exists. 4317 is deliberately NOT published to the host.
  alloy:
    image: grafana/alloy:v1.10.0
    restart: unless-stopped
    command:
      - run
      - --server.http.listen-addr=127.0.0.1:12345
      - --storage.path=/var/lib/alloy/data
      - /etc/alloy/config.alloy
    env_file: .env
    # Hard ceiling on a 2GB box: worst case Alloy OOMs alone rather than
    # taking Postgres with it.
    mem_limit: 256m
    pid: host
    volumes:
      - ./alloy/config.alloy:/etc/alloy/config.alloy:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /:/rootfs:ro
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - alloy_data:/var/lib/alloy/data
    logging:
      driver: "json-file"
      options:
        max-size: "5m"
        max-file: "2"
```

- [ ] **Step 3: Teach remote-deploy.sh to fetch the new parameters**

The script already has a `param()` helper that fetches with `--with-decryption`, so the SecureString token needs no special handling. Add these lines inside the existing `cat > .env <<EOF` heredoc, after the `ACME_EMAIL` line:

```bash
GRAFANA_OTLP_ENDPOINT=$(param /mighty/grafana/otlp_endpoint)
GRAFANA_OTLP_INSTANCE_ID=$(param /mighty/grafana/otlp_instance_id)
GRAFANA_PROM_URL=$(param /mighty/grafana/prom_url)
GRAFANA_PROM_USER_ID=$(param /mighty/grafana/prom_user_id)
GRAFANA_LOKI_URL=$(param /mighty/grafana/loki_url)
GRAFANA_LOKI_USER_ID=$(param /mighty/grafana/loki_user_id)
GRAFANA_TOKEN=$(param /mighty/grafana/token)
MIGHTY_ENV=prod
```

`POSTGRES_CONN` is already written by this heredoc (Task 4 needs it for the Postgres exporter) and `umask 077` already protects the file, so no other changes are required here.

- [ ] **Step 4: Validate the Alloy config syntactically before deploying**

Run locally (no credentials needed — `fmt` only parses):
```bash
docker run --rm -v "$PWD/deploy/compose/alloy:/cfg" \
  grafana/alloy:v1.10.0 fmt /cfg/config.alloy
```
Expected: the formatted config prints with exit code 0. A syntax error prints a line number and exits non-zero. Fix and re-run until clean.

- [ ] **Step 5: Deploy**

```bash
cd deploy/scripts && ./deploy.sh
```
Expected: `remote-deploy.sh` output shows the `alloy` container created and the other five recreated or unchanged.

- [ ] **Step 6: Verify Alloy is healthy on the box**

```bash
INSTANCE=$(cd deploy/terraform && tofu output -raw instance_id)
aws ssm send-command --instance-ids "$INSTANCE" \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["docker ps --format \"{{.Names}} {{.Status}}\"","docker logs --tail 40 $(docker ps -qf name=alloy)"]' \
  --query 'Command.CommandId' --output text
```
Then `aws ssm get-command-invocation --command-id <id> --instance-id "$INSTANCE" --query StandardOutputContent --output text`.

Expected: `alloy` is `Up`, and its logs contain no repeated `error` lines. Authentication failures appear here as 401s from the remote-write endpoint — if you see those, the token or user IDs are wrong; re-check Task 1 Step 1 (the three backends have *different* numeric IDs).

- [ ] **Step 7: Verify host metrics arrived in Grafana Cloud**

In the stack: **Explore → grafanacloud-\<stack\>-prom**, query `node_memory_MemAvailable_bytes{job="mighty/host"}`.
Expected: a series with a plausible value (roughly 0.5–1.5 GB). If empty, the `procfs_path` mounts are wrong.

Also confirm the series budget: query `count({__name__=~".+", job="mighty/host"})`. Expected: a few hundred, not thousands. If over ~1500, trim `set_collectors` further.

- [ ] **Step 8: Verify logs arrived**

**Explore → grafanacloud-\<stack\>-logs**, query `{service="mighty", container="mighty"} | json | level="info"`.
Expected: recent zerolog lines. Then query `{container=~".*alloy.*"}` — expected: **no results**, proving the feedback-loop exclusion works.

- [ ] **Step 9: Commit**

```bash
git add deploy/compose/alloy/config.alloy deploy/compose/docker-compose.prod.yml deploy/compose/remote-deploy.sh
git commit -m "feat(obs): add Alloy gateway shipping host metrics and container logs"
```

---

### Task 3: Discord contact point and Stage-1 alert rules

**Files:**
- Create: `deploy/terraform/grafana.tf`
- Modify: `deploy/terraform/versions.tf`

**Interfaces:**
- Consumes: `var.grafana_url`, `var.grafana_sa_token`, `var.discord_webhook_url`, `var.prom_datasource_uid` from Task 1.
- Produces: `grafana_folder.mighty` (folder UID used by every later rule group); `grafana_contact_point.discord`; `grafana_rule_group.critical` containing the three infra rules.

- [ ] **Step 1: Add the Grafana provider**

Add to the `required_providers` block in `deploy/terraform/versions.tf`:

```hcl
    grafana = {
      source  = "grafana/grafana"
      version = "~> 3.18"
    }
```

- [ ] **Step 2: Write the provider, folder, contact point, and notification policy**

Create `deploy/terraform/grafana.tf`:

```hcl
provider "grafana" {
  url  = var.grafana_url
  auth = var.grafana_sa_token
}

resource "grafana_folder" "mighty" {
  title = "Mighty"
}

resource "grafana_contact_point" "discord" {
  name = "mighty-discord"

  discord {
    url                     = var.discord_webhook_url
    disable_resolve_message = false
  }
}

# NOTE: this resource manages the stack's ROOT notification policy and will
# overwrite whatever is there. That is intended on a fresh stack; if this
# stack is ever shared with other workloads, scope this to a child policy
# matched on a label instead.
resource "grafana_notification_policy" "root" {
  group_by      = ["alertname"]
  contact_point = grafana_contact_point.discord.name

  group_wait      = "30s"
  group_interval  = "5m"
  repeat_interval = "4h"
}
```

- [ ] **Step 3: Add the critical rule group with the three Stage-1 rules**

Append to `deploy/terraform/grafana.tf`.

Every expression must return **exactly one series valued 0 or 1, in every
state**. Use the `bool` modifier on comparisons; do not use bare `absent()`.

This is not stylistic. With `no_data_state = "Alerting"`, an expression that
returns *no series* is indistinguishable from an emergency — so any rule with
a no-series state fires on install and never clears, which trains the operator
to mute the channel. Before adding a rule, walk both states and ask: **can
this return zero series?**

Two traps this plan hit:
- `absent(v)` returns an empty vector when `v` HAS series (it returns 1 only
  when `v` matches nothing). Use `(count(v) * 0) or vector(1)` instead.
- node_exporter strips `rootfs_path` from the exposed `mountpoint` label, so a
  host root bind-mounted at `/rootfs` is still labelled `mountpoint="/"`. That means one uniform threshold condition (`> 0`) works for all rules, and absence of data genuinely means the telemetry path is broken — which is why `no_data_state` is `Alerting` everywhere.

```hcl
locals {
  # Each expr MUST evaluate to 0 or 1 (use the `bool` modifier on
  # comparisons) so the shared `gt 0` threshold below applies uniformly.
  critical_rules = {
    telemetry_blind = {
      title   = "Telemetry blind - no host metrics"
      # NOT absent(): absent(v) returns an EMPTY vector when v has series, not
      # 0. Empty -> No Data -> Alerting, so the healthy state would page and
      # never clear. count() collapses to one label-less series, and `or`
      # suppresses vector(1) because the label sets match.
      expr    = "(count(node_memory_MemAvailable_bytes{job=\"mighty/host\"}) * 0) or vector(1)"
      for     = "10m"
      summary = "No host metrics for 10m: Alloy or the box is gone. Every other alert is now unreliable. Check `docker ps` and Alloy logs over SSM."
    }

    memory_exhausted = {
      title   = "Memory exhausted"
      expr    = "min(node_memory_MemAvailable_bytes{job=\"mighty/host\"}) < bool 150e6"
      for     = "10m"
      summary = "Under 150MB available on a 2GB box. Most likely real incident for this stack; swap turns a crash into slow death. Check Postgres and Alloy RSS."
    }

    disk_filling = {
      title   = "Disk filling"
      expr    = "min(node_filesystem_avail_bytes{job=\"mighty/host\", mountpoint=\"/\"} / node_filesystem_size_bytes{job=\"mighty/host\", mountpoint=\"/\"}) < bool 0.15"
      for     = "15m"
      summary = "Under 15% free on the 20GB root volume. Usual cause is accumulated Docker images: run `docker image prune -f`."
    }
  }
}

resource "grafana_rule_group" "critical" {
  name             = "mighty-critical"
  folder_uid       = grafana_folder.mighty.uid
  interval_seconds = 60

  dynamic "rule" {
    for_each = local.critical_rules

    content {
      name      = rule.value.title
      for       = rule.value.for
      condition = "B"

      # Missing data means the telemetry path itself is broken, which is
      # worth waking up for — not something to silently treat as healthy.
      no_data_state  = "Alerting"
      exec_err_state = "Alerting"

      annotations = {
        summary = rule.value.summary
      }

      labels = {
        severity = "critical"
      }

      data {
        ref_id         = "A"
        datasource_uid = var.prom_datasource_uid

        relative_time_range {
          from = 600
          to   = 0
        }

        model = jsonencode({
          refId         = "A"
          expr          = rule.value.expr
          instant       = true
          intervalMs    = 60000
          maxDataPoints = 43200
        })
      }

      data {
        ref_id         = "B"
        datasource_uid = "__expr__"

        relative_time_range {
          from = 0
          to   = 0
        }

        model = jsonencode({
          refId      = "B"
          type       = "threshold"
          expression = "A"
          conditions = [{
            evaluator = {
              type   = "gt"
              params = [0]
            }
          }]
        })
      }
    }
  }
}
```

- [ ] **Step 4: Apply**

Run: `cd deploy/terraform && tofu init -upgrade && tofu apply`
Expected: creates one folder, one contact point, one notification policy, one rule group with three rules. **No changes to any `aws_*` resource.** If AWS resources appear in the plan, stop and investigate before applying.

- [ ] **Step 5: Verify the contact point actually delivers**

In the stack: **Alerting → Contact points → mighty-discord → Test**. Send the default test notification.
Expected: a message appears in the Discord channel within a few seconds. **Do not proceed until this works** — every alert in this plan depends on it, and an untested delivery path is an assumption, not a safety net.

- [ ] **Step 6: Verify a rule actually fires end to end**

Temporarily invert one threshold so it must fire, apply, wait, confirm Discord, then revert:

```bash
# In grafana.tf, change the disk_filling expr comparison from `< bool 0.15`
# to `> bool 0.15` (i.e. "more than 15% free" — currently true).
cd deploy/terraform && tofu apply
# Wait ~2 minutes (60s interval + 15m `for` — temporarily lower `for` to "1m"
# as well so this test does not take a quarter of an hour).
```
Expected: a Discord message titled with the rule name.

Then revert both edits and `tofu apply` again. Confirm **Alerting → Alert rules** shows the rule back in `Normal` state and a resolved message arrived in Discord.

- [ ] **Step 7: Commit**

```bash
git add deploy/terraform/grafana.tf deploy/terraform/versions.tf
git commit -m "feat(obs): add Discord contact point and infra critical alerts"
```

---

## Stage 2 — Infra exporters (no Go changes)

### Task 4: Postgres and Redis exporters plus saturation alert

**Files:**
- Modify: `deploy/compose/alloy/config.alloy`
- Modify: `deploy/compose/docker-compose.prod.yml`
- Modify: `deploy/compose/remote-deploy.sh`
- Modify: `deploy/terraform/grafana.tf`
- Create: `deploy/grafana/dashboards/.gitkeep`

**Interfaces:**
- Consumes: `grafana_folder.mighty`, `local.critical_rules` pattern from Task 3.
- Produces: metrics under `job="mighty/postgres"` and `job="mighty/redis"`; a fourth critical rule `postgres_saturation`.

- [ ] **Step 1: Add the exporters to the Alloy config**

Append to `deploy/compose/alloy/config.alloy`:

```alloy
// ------------------------------------------------------ postgres & redis
// Both are Alloy built-ins — one container, not two extra sidecars.

prometheus.exporter.postgres "pg" {
  data_source_names = [sys.env("POSTGRES_CONN")]

  // Per-table and per-index stats are the bulk of this exporter's series
  // and are not worth it for a schema this small.
  enabled_collectors = ["database", "stat_database", "stat_bgwriter", "locks"]
}

discovery.relabel "pg" {
  targets = prometheus.exporter.postgres.pg.targets

  rule {
    target_label = "job"
    replacement  = "mighty/postgres"
  }

  rule {
    target_label = "env"
    replacement  = sys.env("MIGHTY_ENV")
  }
}

prometheus.scrape "pg" {
  targets         = discovery.relabel.pg.output
  forward_to      = [prometheus.remote_write.grafana_cloud.receiver]
  scrape_interval = "60s"
}

prometheus.exporter.redis "redis" {
  redis_addr = "redis:6379"
}

discovery.relabel "redis" {
  targets = prometheus.exporter.redis.redis.targets

  rule {
    target_label = "job"
    replacement  = "mighty/redis"
  }

  rule {
    target_label = "env"
    replacement  = sys.env("MIGHTY_ENV")
  }
}

prometheus.scrape "redis" {
  targets         = discovery.relabel.redis.output
  forward_to      = [prometheus.remote_write.grafana_cloud.receiver]
  scrape_interval = "60s"
}
```

- [ ] **Step 2: Give Alloy network access to Postgres and Redis**

`POSTGRES_CONN` is already written to `.env` by `remote-deploy.sh`, and `env_file: .env` is already on the `alloy` service from Task 2, so no new secret plumbing is needed.

Add `depends_on` to the `alloy` service in `docker-compose.prod.yml` so it does not spend its first minute logging connection refusals:

```yaml
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_started
```

- [ ] **Step 3: Validate and deploy**

```bash
docker run --rm -v "$PWD/deploy/compose/alloy:/cfg" grafana/alloy:v1.10.0 fmt /cfg/config.alloy
cd deploy/scripts && ./deploy.sh
```
Expected: `fmt` exits 0; deploy recreates `alloy`.

- [ ] **Step 4: Verify both exporters report**

In **Explore → prom**:
- `pg_up{job="mighty/postgres"}` → expected `1`
- `redis_up{job="mighty/redis"}` → expected `1`
- `count({__name__=~".+", job=~"mighty/(postgres|redis)"})` → expected a few hundred

If `pg_up` is `0`, Alloy cannot authenticate to Postgres — check that `POSTGRES_CONN` in `.env` uses host `postgres`, not `localhost`.

- [ ] **Step 5: Import the free integration dashboards**

In the stack: **Connections → Add new connection → PostgreSQL**, then use its **Install dashboards** action; repeat for **Redis**. These ship maintained dashboards for exactly these exporters — do not rebuild what comes free.

Then set each dashboard's `job` variable to `mighty/postgres` / `mighty/redis` and confirm panels populate.

- [ ] **Step 6: Add the Postgres saturation alert**

Add a fourth entry to `local.critical_rules` in `deploy/terraform/grafana.tf`:

```hcl
    postgres_saturation = {
      title   = "Postgres connection saturation"
      expr    = "max(sum by (job) (pg_stat_database_numbackends{job=\"mighty/postgres\"}) / on (job) group_left max by (job) (pg_settings_max_connections{job=\"mighty/postgres\"})) > bool 0.8"
      for     = "5m"
      summary = "Postgres is above 80% of max_connections. Task 9 refines this with app-side sql.DBStats waiting-connection counts, which lead this signal."
    }
```

If `pg_settings_max_connections` is absent (it requires the `settings` collector), add `"settings"` to `enabled_collectors` in Step 1, redeploy, and re-verify in Explore before applying.

- [ ] **Step 7: Apply and verify the rule is healthy**

Run: `cd deploy/terraform && tofu apply`
Then in **Alerting → Alert rules**, confirm the new rule shows state `Normal` — **not** `Error` or `NoData`. An `Error` state means the PromQL is wrong; paste the expression into Explore to debug it.

- [ ] **Step 8: Commit**

```bash
git add deploy/compose/alloy/config.alloy deploy/compose/docker-compose.prod.yml \
        deploy/compose/remote-deploy.sh deploy/terraform/grafana.tf
git commit -m "feat(obs): add Postgres and Redis exporters with saturation alert"
```

---

## Stage 3 — Application metrics

### Task 5: The `obs` package — OTel lifecycle, no-op by default

**Files:**
- Create: `internal/obs/obs.go`
- Create: `internal/obs/obs_test.go`
- Create: `internal/obs/main_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `type Config struct { ServiceName, ServiceVersion, Environment, Endpoint string; SampleRatio float64 }`
  - `func Init(ctx context.Context, cfg Config) (*Provider, error)`
  - `type Provider struct { Enabled bool; Tracer trace.TracerProvider; Meter metric.MeterProvider }`
  - `func (p *Provider) Shutdown(ctx context.Context) error`
  - `func ConfigFromEnv() Config`

- [ ] **Step 1: Add the dependencies**

```bash
go get go.opentelemetry.io/otel@latest \
       go.opentelemetry.io/otel/sdk@latest \
       go.opentelemetry.io/otel/sdk/metric@latest \
       go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@latest \
       go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@latest \
       go.opentelemetry.io/contrib/instrumentation/runtime@latest
go mod tidy
```

- [ ] **Step 2: Write the failing tests**

Create `internal/obs/main_test.go` — matching the existing pattern in `internal/api/main_test.go`:

```go
package obs

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
	os.Exit(m.Run())
}
```

Create `internal/obs/obs_test.go`:

```go
package obs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// An unset endpoint is the dev and test default: no exporter, no network, no
// goroutines. goleak in TestMain is what actually enforces the last part.
func TestInitDisabledWhenEndpointEmpty(t *testing.T) {
	p, err := Init(context.Background(), Config{ServiceName: "test"})
	require.NoError(t, err)
	require.False(t, p.Enabled)

	// Not SDK providers — no batching goroutines, no export attempts.
	require.NotImplements(t, (*interface{ ForceFlush(context.Context) error })(nil), p.Tracer)

	require.NoError(t, p.Shutdown(context.Background()))
}

// The gRPC exporters connect lazily, so this succeeds with no collector
// listening — which is what makes it testable without a fixture.
func TestInitEnabledBuildsSDKProviders(t *testing.T) {
	p, err := Init(context.Background(), Config{
		ServiceName: "test",
		Endpoint:    "localhost:4317",
		SampleRatio: 1,
	})
	require.NoError(t, err)
	require.True(t, p.Enabled)
	require.IsType(t, &sdktrace.TracerProvider{}, p.Tracer)
	require.IsType(t, &sdkmetric.MeterProvider{}, p.Meter)

	require.NoError(t, p.Shutdown(context.Background()))
}

func TestShutdownIsIdempotent(t *testing.T) {
	p, err := Init(context.Background(), Config{ServiceName: "test", Endpoint: "localhost:4317", SampleRatio: 1})
	require.NoError(t, err)
	require.NoError(t, p.Shutdown(context.Background()))
	require.NoError(t, p.Shutdown(context.Background()))
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")

	cfg := ConfigFromEnv()
	require.Empty(t, cfg.Endpoint)
	require.InDelta(t, 1.0, cfg.SampleRatio, 0.001)
	require.Equal(t, "mighty", cfg.ServiceName)
}

func TestConfigFromEnvReadsSampleRatio(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy:4317")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")

	cfg := ConfigFromEnv()
	require.Equal(t, "alloy:4317", cfg.Endpoint)
	require.InDelta(t, 0.25, cfg.SampleRatio, 0.001)
}

// A malformed ratio must not disable tracing silently — it falls back to 1.
func TestConfigFromEnvRejectsBadSampleRatio(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy:4317")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "banana")

	cfg := ConfigFromEnv()
	require.InDelta(t, 1.0, cfg.SampleRatio, 0.001)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/obs/ -v`
Expected: FAIL — `undefined: Init`, `undefined: Config`, `undefined: ConfigFromEnv`.

- [ ] **Step 4: Implement `obs.go`**

```go
// Package obs owns this service's OpenTelemetry wiring: SDK lifecycle, metric
// instruments, and the bridge between zerolog and the active span.
//
// Everything here is inert unless OTEL_EXPORTER_OTLP_ENDPOINT is set. That is
// deliberate: the dev compose stack has no collector and `go test ./...` must
// not open sockets or spawn exporter goroutines.
package obs

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// ServiceName is the resource name every signal is tagged with.
const ServiceName = "mighty"

// exportInterval matches the Alloy scrape interval. Pushing more often than
// the backend stores costs bandwidth on a small box and buys nothing.
const exportInterval = 60 * time.Second

// Config describes the SDK setup. An empty Endpoint disables everything.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	// Endpoint is an OTLP gRPC address such as "alloy:4317". Empty disables
	// all instrumentation.
	Endpoint string
	// SampleRatio is the parent-based trace sampling ratio, 0..1.
	SampleRatio float64
}

// ConfigFromEnv reads the standard OTEL_* variables. A malformed sample ratio
// falls back to 1 rather than 0: silently sampling nothing looks identical to
// a working system right up until you need a trace.
func ConfigFromEnv() Config {
	cfg := Config{
		ServiceName:    ServiceName,
		ServiceVersion: os.Getenv("MIGHTY_VERSION"),
		Environment:    os.Getenv("MIGHTY_ENV"),
		Endpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		SampleRatio:    1,
	}

	if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 && v <= 1 {
			cfg.SampleRatio = v
		}
	}

	return cfg
}

// Provider holds the configured providers and their shutdown hooks.
type Provider struct {
	// Enabled reports whether real exporters were installed. Callers use it
	// to decide whether to log that telemetry is active.
	Enabled bool
	Tracer  trace.TracerProvider
	Meter   metric.MeterProvider

	shutdownOnce sync.Once
	shutdownFns  []func(context.Context) error
	shutdownErr  error
}

// Init builds the providers and installs them globally. With cfg.Endpoint
// empty it installs no-op providers and returns a Provider whose Shutdown is
// a no-op, so callers need no conditional wiring.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = ServiceName
	}

	if cfg.Endpoint == "" {
		p := &Provider{
			Enabled: false,
			Tracer:  tracenoop.NewTracerProvider(),
			Meter:   metricnoop.NewMeterProvider(),
		}
		otel.SetTracerProvider(p.Tracer)
		otel.SetMeterProvider(p.Meter)

		return p, nil
	}

	attrs := []attribute.KeyValue{semconv.ServiceName(cfg.ServiceName)}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}

	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(cfg.Environment))
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, attrs...))
	if err != nil {
		return nil, err
	}

	// WithInsecure: the collector is a sibling container on a private compose
	// network. TLS here would protect a hop that never leaves the host, and
	// Alloy is the component that speaks TLS to Grafana Cloud.
	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithInsecure())
	if err != nil {
		return nil, errors.Join(err, traceExp.Shutdown(ctx))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(exportInterval))),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	return &Provider{
		Enabled:     true,
		Tracer:      tp,
		Meter:       mp,
		shutdownFns: []func(context.Context) error{tp.Shutdown, mp.Shutdown},
	}, nil
}

// Shutdown flushes and stops the exporters. Safe to call more than once.
func (p *Provider) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		var errs []error
		for _, fn := range p.shutdownFns {
			errs = append(errs, fn(ctx))
		}

		p.shutdownErr = errors.Join(errs...)
	})

	return p.shutdownErr
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/obs/ -v`
Expected: PASS, all six tests, with no goleak failures.

If goleak reports leaked gRPC goroutines, `Shutdown` is not being reached — check that `errors.Join` is not short-circuiting.

- [ ] **Step 6: Confirm nothing else broke**

Run: `go test ./... && golangci-lint run`
Expected: all packages PASS, lint clean.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/obs/
git commit -m "feat(obs): add OTel SDK lifecycle, no-op unless OTLP endpoint set"
```

---

### Task 6: Trace-aware logging and the collapsed request log

**Files:**
- Create: `internal/obs/log.go`
- Create: `internal/obs/log_test.go`
- Modify: `internal/api/handler.go` (`LoggingMiddleware`, around lines 363–412)
- Modify: `internal/api/handler_test.go` (`TestLoggingMiddleware`)

**Interfaces:**
- Consumes: nothing from Task 5 at runtime; shares the package.
- Produces: `func Log(ctx context.Context) *zerolog.Logger`

- [ ] **Step 1: Write the failing tests**

Create `internal/obs/log_test.go`:

```go
package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// captureLogs swaps the global zerolog logger for one writing to a buffer and
// returns the decoded single line that was written.
func captureLog(t *testing.T, fn func()) map[string]any {
	t.Helper()

	var buf bytes.Buffer

	original := zlog.Logger
	zlog.Logger = zerolog.New(&buf)

	t.Cleanup(func() { zlog.Logger = original })

	fn()

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))

	return out
}

func TestLogAddsTraceIDWhenSpanPresent(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	out := captureLog(t, func() {
		Log(ctx).Info().Msg("hello")
	})

	require.Equal(t, span.SpanContext().TraceID().String(), out["trace_id"])
	require.Equal(t, span.SpanContext().SpanID().String(), out["span_id"])
}

func TestLogOmitsTraceIDWithoutSpan(t *testing.T) {
	out := captureLog(t, func() {
		Log(context.Background()).Info().Msg("hello")
	})

	require.NotContains(t, out, "trace_id")
	require.NotContains(t, out, "span_id")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/obs/ -run TestLog -v`
Expected: FAIL — `undefined: Log`.

- [ ] **Step 3: Implement `log.go`**

```go
package obs

import (
	"context"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

// Log returns the global logger with trace_id and span_id bound when ctx
// carries a valid span, and unchanged otherwise.
//
// This is an explicit helper rather than a zerolog.Hook because zerolog hooks
// receive no context: a hook could only read a ctx that call sites had already
// attached, which is the same call-site change with more indirection.
//
// trace_id is a log FIELD, never a Loki label — it is unbounded and would
// create one log stream per trace.
//
// Returns *zerolog.Logger, not a value: zerolog's Info/Warn/Error have pointer
// receivers, and a function's return value is not addressable, so a value
// return would make the chained obs.Log(ctx).Warn() form used throughout
// Tasks 8 and 11 fail to compile.
func Log(ctx context.Context) *zerolog.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		// Address of the package global, not a copy: keeps the zero-overhead
		// path allocation-free and observes later reassignment of the global.
		return &zlog.Logger
	}

	l := zlog.Logger.With().
		Str("trace_id", sc.TraceID().String()).
		Str("span_id", sc.SpanID().String()).
		Logger()

	return &l
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/obs/ -run TestLog -v`
Expected: PASS.

- [ ] **Step 5: Collapse `LoggingMiddleware` to one line per request**

Replace the body of `LoggingMiddleware` in `internal/api/handler.go` (currently lines 370–412). Keep the existing doc comment above it verbatim — it explains why this is a method rather than a package function — and add `github.com/joekhosbayar/go-mighty/internal/obs` to the file's imports.

```go
func (h *Handler) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		lrw := &LoggingResponseWriter{ResponseWriter: w}
		start := time.Now()

		next.ServeHTTP(lrw, req)

		// If responseCode is 0, neither WriteHeader nor Write was called.
		// This should be treated as 200 OK per HTTP specification.
		statusCode := lrw.responseCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		// One line per request, not two. The old pre-flight "Incoming
		// request" line duplicated everything below it; otelhttp now records
		// count, status and duration as metrics, so paying to ship the same
		// facts twice against a 50GB log budget buys nothing.
		logger := obs.Log(req.Context())

		event := logger.Info()
		msg := "Success response"

		// All five cases below already exist in the current code. Keep every
		// one of them, and keep the field name `responseCode` — renaming it
		// breaks the existing test and any log query built on it.
		switch {
		case statusCode >= 500:
			event = logger.Error()
			msg = "5xx response"
		case statusCode >= 400:
			event = logger.Warn()
			msg = "4xx response"
		case statusCode >= 300:
			msg = "3xx redirection response"
		case statusCode >= 100 && statusCode < 200:
			msg = "1xx informational response"
		}

		event.
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Str("remote", ClientIP(req, h.trustProxy)).
			Int("responseCode", statusCode).
			Dur("duration", time.Since(start)).
			Msg(msg)
	})
}
```

Read lines 400–412 of the current file before pasting and carry over the exact `msg` strings and field names it uses for the 4xx case, so existing log-based queries and the test in Step 6 stay aligned.

- [ ] **Step 6: Update `TestLoggingMiddleware`**

`internal/api/handler_test.go:153` currently asserts against two emitted lines. Update it to assert exactly **one** line per request, still checking status-to-level mapping and the presence of `duration`. Add one case asserting `method`, `url`, and `remote` are present on that single line, so the field migration is covered.

- [ ] **Step 7: Run the API tests**

Run: `go test ./internal/api/ -v -run TestLoggingMiddleware`
Expected: PASS.

Then `go test ./... && golangci-lint run` — expected all PASS, lint clean.

- [ ] **Step 8: Commit**

```bash
git add internal/obs/log.go internal/obs/log_test.go internal/api/handler.go internal/api/handler_test.go
git commit -m "feat(obs): bind trace IDs to logs and collapse duplicate request log line"
```

---

### Task 7: Metric instruments and the cardinality guard

**Files:**
- Create: `internal/obs/metrics.go`
- Create: `internal/obs/metrics_test.go`

**Interfaces:**
- Consumes: `Provider.Meter` from Task 5.
- Produces:
  - `func NewMetrics(mp metric.MeterProvider) (*Metrics, error)`
  - Nil-safe recorders: `(*Metrics).RecordHandshake(ctx, kind, outcome string)`, `.AddConnection(ctx, kind string, delta int64)`, `.RecordWSMessage(ctx, msgType, outcome string)`, `.RecordRateLimitRejection(ctx, bucket string)`, `.RecordGameCreated(ctx)`, `.RecordMove(ctx, outcome string, seconds float64)`, `.RecordOptimisticConflict(ctx)`
  - Outcome constants: `OutcomeOK`, `OutcomeAuthTimeout`, `OutcomeAuthFailed`, `OutcomeAuthUnavailable`, `OutcomeOriginRejected`, `OutcomeUpgradeFailed`, `OutcomeConnLimitUser`, `OutcomeConnLimitIP`, `MsgAccepted`, `MsgRateLimited`, `MsgInvalid`, `MoveValid`, `MoveInvalid`, `MoveConflict`, `MoveError`
  - Kind constants: `KindGame`, `KindLobby`

- [ ] **Step 1: Write the failing tests**

Create `internal/obs/metrics_test.go`:

```go
package obs

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collect gathers all metrics recorded through a manual reader.
func collect(t *testing.T, fn func(*Metrics)) metricdata.ResourceMetrics {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	m, err := NewMetrics(mp)
	require.NoError(t, err)

	fn(m)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	return rm
}

// findSum returns the data points of a named Int64 counter.
func findSum(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %s is not an int64 sum", name)

			return sum.DataPoints
		}
	}

	t.Fatalf("metric %s not recorded", name)

	return nil
}

func TestRecordHandshakeCarriesOutcomeAndKind(t *testing.T) {
	// The (Lobby, OK) call is load-bearing: without it, kind and outcome
	// co-vary perfectly and the test would still pass against an
	// implementation that dropped the kind attribute entirely.
	rm := collect(t, func(m *Metrics) {
		m.RecordHandshake(context.Background(), KindGame, OutcomeOK)
		m.RecordHandshake(context.Background(), KindGame, OutcomeOK)
		m.RecordHandshake(context.Background(), KindLobby, OutcomeOK)
		m.RecordHandshake(context.Background(), KindLobby, OutcomeOriginRejected)
	})

	points := findSum(t, rm, "mighty.ws.handshake")
	require.Len(t, points, 2, "expected one series per (kind, outcome) pair")

	byOutcome := map[string]int64{}

	for _, p := range points {
		outcome, ok := p.Attributes.Value("outcome")
		require.True(t, ok)
		byOutcome[outcome.AsString()] = p.Value
	}

	require.Equal(t, int64(2), byOutcome[OutcomeOK])
	require.Equal(t, int64(1), byOutcome[OutcomeOriginRejected])
}

func TestAddConnectionTracksActiveCount(t *testing.T) {
	rm := collect(t, func(m *Metrics) {
		m.AddConnection(context.Background(), KindGame, 1)
		m.AddConnection(context.Background(), KindGame, 1)
		m.AddConnection(context.Background(), KindGame, -1)
	})

	points := findSum(t, rm, "mighty.ws.connections.active")
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
}

func TestRecordMoveEmitsCounterAndHistogram(t *testing.T) {
	rm := collect(t, func(m *Metrics) {
		m.RecordMove(context.Background(), MoveValid, 0.012)
	})

	require.Len(t, findSum(t, rm, "mighty.moves"), 1)

	var found bool

	for _, sm := range rm.ScopeMetrics {
		for _, mm := range sm.Metrics {
			if mm.Name == "mighty.move.duration" {
				found = true
			}
		}
	}

	require.True(t, found, "move duration histogram not recorded")
}

// Every recorder must tolerate a nil receiver: existing tests construct
// api.NewHandler with no options, so the metrics pointer is legitimately nil.
func TestNilMetricsIsSafe(t *testing.T) {
	var m *Metrics

	ctx := context.Background()

	require.NotPanics(t, func() {
		m.RecordHandshake(ctx, KindGame, OutcomeOK)
		m.AddConnection(ctx, KindGame, 1)
		m.RecordWSMessage(ctx, "MOVE", MsgAccepted)
		m.RecordRateLimitRejection(ctx, "creategame")
		m.RecordGameCreated(ctx)
		m.RecordMove(ctx, MoveValid, 0.1)
		m.RecordOptimisticConflict(ctx)
	})
}

// The cardinality rule from the spec, enforced rather than commented.
// game_id / user_id / IP on a metric label is unbounded and would consume the
// 10k free-tier series budget, after which data is silently dropped.
func TestNoForbiddenMetricLabels(t *testing.T) {
	src, err := os.ReadFile("metrics.go")
	require.NoError(t, err)

	for _, forbidden := range []string{
		`"game_id"`, `"user_id"`, `"ip"`, `"client_ip"`, `"remote"`, `"conn_id"`,
	} {
		require.NotContains(t, string(src), forbidden,
			"%s must never be a metric label - it is unbounded. Put it on a span or a log field instead.", forbidden)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/obs/ -run 'TestRecord|TestAdd|TestNil|TestNoForbidden' -v`
Expected: FAIL — `undefined: NewMetrics`, `undefined: Metrics`.

- [ ] **Step 3: Implement `metrics.go`**

```go
package obs

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Bounded label values. Every attribute in this file must come from a fixed
// set: this is the only file allowed to name metric attributes, and the
// cardinality guard test in metrics_test.go scans it for unbounded keys.
const (
	KindGame  = "game"
	KindLobby = "lobby"

	OutcomeOK              = "ok"
	OutcomeAuthTimeout     = "auth_timeout"
	OutcomeAuthFailed      = "auth_failed"
	OutcomeAuthUnavailable = "auth_unavailable"
	OutcomeOriginRejected  = "origin_rejected"
	OutcomeUpgradeFailed   = "upgrade_failed"
	OutcomeConnLimitUser   = "conn_limit_user"
	OutcomeConnLimitIP     = "conn_limit_ip"

	MsgAccepted    = "accepted"
	MsgRateLimited = "rate_limited"
	MsgInvalid     = "invalid"

	// MsgTypeUnknown labels a frame rejected before its type could be read.
	MsgTypeUnknown = "unknown"

	MoveValid    = "valid"
	MoveInvalid  = "invalid"
	MoveConflict = "conflict"
	MoveError    = "error"
)

// Metrics holds every application instrument. A nil *Metrics is valid and
// every method is a no-op on it, so callers that were constructed without
// telemetry (local dev, the existing test suite) need no branching.
type Metrics struct {
	wsHandshake         metric.Int64Counter
	wsConnectionsActive metric.Int64UpDownCounter
	wsMessages          metric.Int64Counter
	rateLimitRejections metric.Int64Counter
	gamesCreated        metric.Int64Counter
	moves               metric.Int64Counter
	moveDuration        metric.Float64Histogram
	optimisticConflicts metric.Int64Counter
}

// NewMetrics registers the instruments on mp.
func NewMetrics(mp metric.MeterProvider) (*Metrics, error) {
	meter := mp.Meter("github.com/joekhosbayar/go-mighty/internal/obs")

	var (
		m   Metrics
		err error
	)

	if m.wsHandshake, err = meter.Int64Counter("mighty.ws.handshake",
		metric.WithDescription("WebSocket handshake attempts by outcome"),
	); err != nil {
		return nil, err
	}

	if m.wsConnectionsActive, err = meter.Int64UpDownCounter("mighty.ws.connections.active",
		metric.WithDescription("Currently open WebSocket connections"),
	); err != nil {
		return nil, err
	}

	if m.wsMessages, err = meter.Int64Counter("mighty.ws.messages",
		metric.WithDescription("Inbound WebSocket frames by type and transport-level outcome"),
	); err != nil {
		return nil, err
	}

	if m.rateLimitRejections, err = meter.Int64Counter("mighty.ratelimit.rejections",
		metric.WithDescription("HTTP requests rejected by the per-user rate limiter"),
	); err != nil {
		return nil, err
	}

	if m.gamesCreated, err = meter.Int64Counter("mighty.games.created",
		metric.WithDescription("Games created"),
	); err != nil {
		return nil, err
	}

	if m.moves, err = meter.Int64Counter("mighty.moves",
		metric.WithDescription("Moves processed by engine-level outcome"),
	); err != nil {
		return nil, err
	}

	if m.moveDuration, err = meter.Float64Histogram("mighty.move.duration",
		metric.WithDescription("ProcessMove latency"),
		metric.WithUnit("s"),
	); err != nil {
		return nil, err
	}

	if m.optimisticConflicts, err = meter.Int64Counter("mighty.optimistic_conflicts",
		metric.WithDescription("Version conflicts rejected by optimistic concurrency control"),
	); err != nil {
		return nil, err
	}

	return &m, nil
}

// RecordHandshake counts one WebSocket handshake attempt.
func (m *Metrics) RecordHandshake(ctx context.Context, kind, outcome string) {
	if m == nil {
		return
	}

	m.wsHandshake.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("outcome", outcome),
	))
}

// AddConnection adjusts the live connection gauge; pass -1 on close.
func (m *Metrics) AddConnection(ctx context.Context, kind string, delta int64) {
	if m == nil {
		return
	}

	m.wsConnectionsActive.Add(ctx, delta, metric.WithAttributes(attribute.String("kind", kind)))
}

// RecordWSMessage counts one inbound frame at the transport level. Engine
// verdicts on a move belong on RecordMove instead.
func (m *Metrics) RecordWSMessage(ctx context.Context, msgType, outcome string) {
	if m == nil {
		return
	}

	m.wsMessages.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", msgType),
		attribute.String("outcome", outcome),
	))
}

// RecordRateLimitRejection counts one per-user rate-limit rejection. bucket is
// the action name, which is a compile-time constant at every call site.
func (m *Metrics) RecordRateLimitRejection(ctx context.Context, bucket string) {
	if m == nil {
		return
	}

	m.rateLimitRejections.Add(ctx, 1, metric.WithAttributes(attribute.String("bucket", bucket)))
}

// RecordGameCreated counts one created game.
func (m *Metrics) RecordGameCreated(ctx context.Context) {
	if m == nil {
		return
	}

	m.gamesCreated.Add(ctx, 1)
}

// RecordMove counts one processed move and records its latency in seconds.
func (m *Metrics) RecordMove(ctx context.Context, outcome string, seconds float64) {
	if m == nil {
		return
	}

	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	m.moves.Add(ctx, 1, attrs)
	m.moveDuration.Record(ctx, seconds, attrs)
}

// RecordOptimisticConflict counts one rejected stale-version write.
func (m *Metrics) RecordOptimisticConflict(ctx context.Context) {
	if m == nil {
		return
	}

	m.optimisticConflicts.Add(ctx, 1)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/obs/ -v`
Expected: PASS, all tests including `TestNoForbiddenMetricLabels`.

- [ ] **Step 5: Verify the guard test actually guards**

Temporarily add `attribute.String("game_id", "x")` to `RecordMove`'s attributes and run `go test ./internal/obs/ -run TestNoForbiddenMetricLabels`.
Expected: FAIL with the cardinality message. **Remove the temporary line and re-run to confirm PASS.** A guard test that cannot fail is not a guard.

- [ ] **Step 6: Commit**

```bash
git add internal/obs/metrics.go internal/obs/metrics_test.go
git commit -m "feat(obs): add app metric instruments with cardinality guard test"
```

---

### Task 8: WebSocket handshake and message instrumentation

**Files:**
- Modify: `internal/api/options.go`
- Modify: `internal/api/handler.go` (the `Handler` struct)
- Modify: `internal/api/ws.go`
- Modify: `internal/api/lobby_ws.go`
- Modify: `internal/api/ws_hardening_test.go`

**Interfaces:**
- Consumes: `obs.Metrics` and its constants from Task 7.
- Produces: `func WithMetrics(m *obs.Metrics) Option`; `func wsKindFromPath(p string) string`.

- [ ] **Step 1: Write the failing tests**

`internal/api/ws_hardening_test.go` already drives auth timeout, origin rejection, and both connection caps via `setupWSTestHandler(t)` (defined in `ws_test.go:106`) and `serveWS(t, handler)`. Extend it rather than duplicating that setup. Note this file uses stdlib `t.Fatalf` assertions rather than testify — match that style.

Add this helper to `ws_hardening_test.go`:

```go
// wsMetricsHandler returns a handler wired to a manual metric reader, plus a
// counts function that reads one instrument's totals keyed by an attribute.
//
// setupWSTestHandler builds the handler with no options, and the existing
// tests in this file apply options post-construction (see
// TestWSHandlerRejectsDisallowedOrigin), so WithMetrics is applied the same way.
func wsMetricsHandler(t *testing.T) (*Handler, func(instrument, attrKey string) map[string]int64) {
	t.Helper()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	m, err := obs.NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	WithMetrics(m)(handler)

	counts := func(instrument, attrKey string) map[string]int64 {
		t.Helper()

		var rm metricdata.ResourceMetrics
		if collectErr := reader.Collect(context.Background(), &rm); collectErr != nil {
			t.Fatalf("collect: %v", collectErr)
		}

		out := map[string]int64{}

		for _, sm := range rm.ScopeMetrics {
			for _, mm := range sm.Metrics {
				if mm.Name != instrument {
					continue
				}

				sum, ok := mm.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s is not an int64 sum", instrument)
				}

				for _, p := range sum.DataPoints {
					if v, found := p.Attributes.Value(attribute.Key(attrKey)); found {
						out[v.AsString()] += p.Value
					}
				}
			}
		}

		return out
	}

	return handler, counts
}
```

New imports for this file: `context`, `github.com/joekhosbayar/go-mighty/internal/obs`, `go.opentelemetry.io/otel/attribute`, `sdkmetric "go.opentelemetry.io/otel/sdk/metric"`, `go.opentelemetry.io/otel/sdk/metric/metricdata`.

Then add these cases. Each reuses an existing scenario in this file — read that test first and copy its dial/auth sequence, substituting `wsMetricsHandler(t)` for `setupWSTestHandler(t)`:



Every assertion below reads `got := counts("mighty.ws.handshake", "outcome")` after the scenario runs.

| Test name | Scenario (already exercised in this file) | Assertion |
|---|---|---|
| `TestHandshakeMetricAuthTimeout` | connect, send nothing, wait past the 5s auth deadline | `got[obs.OutcomeAuthTimeout] == 1` |
| `TestHandshakeMetricAuthFailed` | send `{"type":"AUTH","token":"bad"}` (make `fakeValidator` return `service.ErrInvalidToken`) | `got[obs.OutcomeAuthFailed] == 1` |
| `TestHandshakeMetricOriginRejected` | `WithAllowedOrigins([]string{"https://themighty.gg"})(handler)`, then `dialWSWithOrigin(t, server, "https://evil.example")` | `got[obs.OutcomeOriginRejected] == 1` **and `got[obs.OutcomeUpgradeFailed] == 0`** — without the second assertion the test passes against a double-counting implementation |
| `TestHandshakeMetricConnLimitUser` | `WithConnLimits(1, 0)(handler)`, two authenticated connections for one user | `got[obs.OutcomeConnLimitUser] == 1` and `got[obs.OutcomeOK] == 1` |
| `TestHandshakeMetricConnLimitIP` | `WithConnLimits(0, 1)(handler)`, two authenticated connections | `got[obs.OutcomeConnLimitIP] == 1` |
| `TestHandshakeMetricOK` | one successful authenticated connection | `got[obs.OutcomeOK] == 1` |

Add one message-level test, `TestWSMessageMetricInvalidFrame`: authenticate successfully, send the literal bytes `not json`, then assert `counts("mighty.ws.messages", "outcome")[obs.MsgInvalid] == 1`.

Note the connection-cap and OK cases need a brief wait or synchronisation before collecting — the handshake counter is incremented on the server goroutine. `fakeWSGameService` already exposes `processMoveCh` for this kind of synchronisation; for handshakes, wait until the client observes the close frame (cap cases) or a successful read/write (OK case) rather than sleeping a fixed duration.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestHandshakeMetric -v`
Expected: FAIL — `undefined: WithMetrics`.

- [ ] **Step 3: Add the option and struct field**

In `internal/api/handler.go`, add to the `Handler` struct:

```go
	metrics          *obs.Metrics
```

In `internal/api/options.go`:

```go
// WithMetrics installs the OTel instruments. Without it the handler records
// nothing: *obs.Metrics methods are nil-safe, which keeps local dev and the
// existing tests free of telemetry wiring.
func WithMetrics(m *obs.Metrics) Option {
	return func(h *Handler) { h.metrics = m }
}
```

- [ ] **Step 4: Instrument `ws.go`**

Add `wsKindFromPath` near `checkOrigin`:

```go
// wsKindFromPath labels a handshake as game or lobby. checkOrigin runs before
// either handler body, so the path is the only signal available there.
func wsKindFromPath(p string) string {
	if strings.HasSuffix(p, "/lobby/ws") {
		return obs.KindLobby
	}

	return obs.KindGame
}
```

Then add these recordings. Every one is placed on an **existing** branch — no new control flow.

**Record the handshake outcome in exactly ONE place — the upgrade-error
branch — and do NOT record inside `checkOrigin`.** `checkOrigin` stays a pure
predicate.

Do not try to distinguish the failure cause from `Upgrade`'s error value:
gorilla's `websocket.ErrBadHandshake` lives in `client.go` and is only ever
returned by the **client** `Dialer`. Server-side `Upgrade` returns a distinct
`websocket.HandshakeError`, so `errors.Is(err, websocket.ErrBadHandshake)` is
always false — a guard written that way is dead code and every origin
rejection gets counted twice.

Capture the cause where it is actually known. `Upgrade` calls `CheckOrigin`
synchronously on the same goroutine, so an unsynchronised flag is safe:

```go
// upgraderFor reports origin rejection through originRejected, because
// Upgrade's error value cannot distinguish the cause (see above).
func (h *Handler) upgraderFor(originRejected *bool) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			allowed := h.checkOrigin(r)
			if !allowed {
				*originRejected = true
			}

			return allowed
		},
	}
}
```

and in each handler:

```go
	var originRejected bool

	up := h.upgraderFor(&originRejected)

	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		outcome := obs.OutcomeUpgradeFailed
		if originRejected {
			outcome = obs.OutcomeOriginRejected
		}

		h.metrics.RecordHandshake(r.Context(), obs.KindGame, outcome)
		// keep the existing log line unchanged
		return
	}
```

In the auth-read error branch, inside the existing timeout/else split:
- timeout → `h.metrics.RecordHandshake(r.Context(), obs.KindGame, obs.OutcomeAuthTimeout)`
- else → `h.metrics.RecordHandshake(r.Context(), obs.KindGame, obs.OutcomeAuthFailed)`

In the `json.Unmarshal` / `authReq.Type != "AUTH"` branch → `obs.OutcomeAuthFailed`.

In the `ValidateToken` error branch, inside the existing `errors.Is(err, service.ErrInvalidToken)` split:
- invalid token → `obs.OutcomeAuthFailed`
- else → `obs.OutcomeAuthUnavailable`

In the `h.conns.acquire` error branch, after the existing `log.Warn`:
```go
			outcome := obs.OutcomeConnLimitIP
			if errors.Is(connErr, errTooManyUserConns) {
				outcome = obs.OutcomeConnLimitUser
			}

			h.metrics.RecordHandshake(r.Context(), obs.KindGame, outcome)
```

Immediately after the connection-limit block (the point past which the connection is fully established):
```go
	h.metrics.RecordHandshake(r.Context(), obs.KindGame, obs.OutcomeOK)
	h.metrics.AddConnection(r.Context(), obs.KindGame, 1)

	defer h.metrics.AddConnection(r.Context(), obs.KindGame, -1)
```

In the read loop:
- after the rate-limit `closeWithCode`, before `break`: `h.metrics.RecordWSMessage(r.Context(), obs.MsgTypeUnknown, obs.MsgRateLimited)`
- in the `json.Unmarshal` failure branch: `h.metrics.RecordWSMessage(r.Context(), obs.MsgTypeUnknown, obs.MsgInvalid)`
- in the `ConvertPayload` failure branch: `h.metrics.RecordWSMessage(r.Context(), wsMessageTypeLabel(inMsg.Type), obs.MsgInvalid)`
- immediately after a successful `json.Unmarshal` of a frame that reaches `ProcessMove`: `h.metrics.RecordWSMessage(r.Context(), wsMessageTypeLabel(inMsg.Type), obs.MsgAccepted)`

**`inMsg.Type` must NEVER reach a metric attribute unsanitised.** It is a raw
client-supplied JSON string, so passing it through would let any authenticated
client mint unlimited metric series by sending random `type` values — silently
exhausting the 10k free-tier budget, after which Grafana Cloud drops data. The
cardinality guard test cannot catch this: it scans attribute *key* names, not
values. Add an allowlist helper next to `wsKindFromPath`:

```go
// wsMessageTypeLabel collapses a client-supplied message type to a bounded
// label set. inMsg.Type is arbitrary attacker-controlled JSON; using it
// directly as a metric attribute would let one client mint unlimited series
// and exhaust the free-tier budget, after which data is dropped silently.
func wsMessageTypeLabel(t string) string {
	switch t {
	case WSMessageTypeMove, WSMessageTypeError:
		return t
	default:
		return obs.MsgTypeUnknown
	}
}
```

`ProcessMove`'s own verdict is **not** recorded here — Task 9 records it as `mighty.moves` in the service layer, where the HTTP path is covered too.

- [ ] **Step 5: Instrument `lobby_ws.go`**

Apply the same recordings with `obs.KindLobby`, at the structurally identical branches: upgrade failure, auth timeout, auth read failure, non-AUTH message, invalid token, auth unavailable, both connection caps, and success plus the deferred `-1`.

The lobby read loop only drains pongs and close frames, so it records no `mighty.ws.messages`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/api/ -v`
Expected: PASS — the new handshake tests plus every pre-existing test in the package unchanged.

- [ ] **Step 7: Lint and full suite**

Run: `go test ./... && golangci-lint run`
Expected: PASS, clean.

- [ ] **Step 8: Commit**

```bash
git add internal/api/options.go internal/api/handler.go internal/api/ws.go \
        internal/api/lobby_ws.go internal/api/ws_hardening_test.go
git commit -m "feat(obs): instrument WebSocket handshake outcomes and inbound frames"
```

---

### Task 9: HTTP, rate-limit, and engine metrics; wire up `main.go`

**Files:**
- Modify: `internal/api/ratelimit_middleware.go`
- Modify: `internal/service/game_service.go`
- Modify: `cmd/server/main.go`
- Create: `internal/api/otelhttp_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `obs.Init`, `obs.NewMetrics`, `api.WithMetrics` from Tasks 5–8.
- Produces: `func service.WithMetrics(m *obs.Metrics) service.Option`; a fully instrumented server binary.

- [ ] **Step 1: Add the HTTP instrumentation dependency**

```bash
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@latest
go mod tidy
```

- [ ] **Step 2: Write the failing WebSocket-through-otelhttp regression test**

This is the test that protects against the deviation noted at the top of this plan: `otelhttp`'s response-writer wrapper does not reliably implement `http.Hijacker`, and `gorilla/websocket` requires it. Without the filter, every WebSocket breaks.

Create `internal/api/otelhttp_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// otelhttp's ResponseWriter wrapper does not reliably implement
// http.Hijacker, which gorilla/websocket needs to take over the connection.
// TraceFilter must therefore exclude every WS route. If this test fails,
// production WebSockets are broken.
func TestWebSocketUpgradeSurvivesOtelHTTPWrapping(t *testing.T) {
	t.Parallel()

	upgraded := make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /games/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		upgraded <- struct{}{}
	})

	wrapped := otelhttp.NewHandler(mux, "mighty", otelhttp.WithFilter(TraceFilter))
	srv := httptest.NewServer(wrapped)

	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/games/abc/ws"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err, "upgrade failed through otelhttp: TraceFilter must exclude WS routes")

	defer func() { _ = conn.Close() }()

	require.Len(t, upgraded, 1)
}

func TestTraceFilterExcludesHealthzAndWS(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"/healthz":         false,
		"/games/abc/ws":    false,
		"/lobby/ws":        false,
		"/games":           true,
		"/games/abc":       true,
		"/games/abc/join":  true,
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		require.Equal(t, want, TraceFilter(req), "path %s", path)
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/api/ -run 'TestWebSocketUpgradeSurvives|TestTraceFilter' -v`
Expected: FAIL — `undefined: TraceFilter`.

- [ ] **Step 4: Add `TraceFilter`**

Append to `internal/api/hardening.go`:

```go
// TraceFilter decides which requests otelhttp instruments.
//
// WebSocket routes are excluded because otelhttp's ResponseWriter wrapper does
// not reliably implement http.Hijacker, which gorilla/websocket requires —
// wrapping them would break every socket. WS traffic is instrumented directly
// in ws.go instead, where a per-message span is the right shape anyway.
//
// /healthz is excluded because the Route 53 probe hits it every 30s and would
// otherwise contribute ~2900 meaningless spans a day against the trace budget.
func TraceFilter(r *http.Request) bool {
	if r.URL.Path == "/healthz" {
		return false
	}

	return !strings.HasSuffix(r.URL.Path, "/ws")
}
```

Ensure `strings` and `net/http` are imported in that file.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/api/ -run 'TestWebSocketUpgradeSurvives|TestTraceFilter' -v`
Expected: PASS.

- [ ] **Step 6: Record rate-limit rejections**

In `internal/api/ratelimit_middleware.go`, inside the existing `if !decision.Allowed` block after the `log.Warn`:

```go
				h.metrics.RecordRateLimitRejection(r.Context(), action)
```

`action` is a compile-time constant at every call site (`"creategame"`), so this label is bounded.

- [ ] **Step 7: Add engine metrics to the service layer**

In `internal/service/game_service.go`, add an options pattern mirroring `api.Option`:

```go
// Option configures a Game service at construction time.
type Option func(*Game)

// WithMetrics installs the OTel instruments. Methods on *obs.Metrics are
// nil-safe, so omitting this leaves the service uninstrumented.
func WithMetrics(m *obs.Metrics) Option {
	return func(g *Game) { g.metrics = m }
}
```

Add `metrics *obs.Metrics` to the `Game` struct and make `NewGame` variadic:

```go
func NewGame(r RedisStore, p *postgres.Store, opts ...Option) *Game {
	g := &Game{ /* existing fields unchanged */ }

	for _, opt := range opts {
		opt(g)
	}

	return g
}
```

Variadic options keep every existing `NewGame(redisStore, pgStore)` call site — including all the tests — compiling unchanged.

In `CreateGame`, immediately before the final `return g, nil`:
```go
	s.metrics.RecordGameCreated(ctx)
```

In `ProcessMove`, wrap the whole body's outcome with a named-return deferred recorder. Change the signature to use named returns and add at the top, immediately after the signature:

```go
	start := time.Now()

	// Classify every exit path once, here, rather than at each return.
	defer func() {
		outcome := obs.MoveValid

		switch {
		case err == nil:
			// valid
		case errors.Is(err, redisstore.ErrStaleVersion):
			outcome = obs.MoveConflict

			s.metrics.RecordOptimisticConflict(ctx)
		case errors.Is(err, game.ErrInvalidMove):
			outcome = obs.MoveInvalid
		default:
			outcome = obs.MoveError
		}

		s.metrics.RecordMove(ctx, outcome, time.Since(start).Seconds())
	}()
```

Before writing this, grep the `game` package for the sentinel error that `ValidateMove` and `ApplyMove` return: `grep -n "ErrInvalid\|errors.New" internal/game/*.go`. If there is no single sentinel, classify by checking the concrete error returned from those two calls instead — set a local `invalidMove = true` flag in those two branches and read it in the deferred func. **Do not** classify by string matching on the error message.

- [ ] **Step 8: Wire everything into `main.go`**

In `cmd/server/main.go`:

After the logging config block and before the store setup, initialise telemetry:

```go
	obsCfg := obs.ConfigFromEnv()

	obsProvider, err := obs.Init(ctx, obsCfg)
	if err != nil {
		log.Fatalf("observability init: %v", err)
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if shutdownErr := obsProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			zlog.Warn().Err(shutdownErr).Msg("observability shutdown")
		}
	}()

	metrics, err := obs.NewMetrics(obsProvider.Meter)
	if err != nil {
		log.Fatalf("observability metrics: %v", err)
	}
```

Note this needs `ctx` to exist earlier than it currently does — move the existing `ctx := context.Background()` up to just before this block and delete the later declaration.

Add Go runtime metrics — goroutine count, heap, and GC pause are the difference between diagnosing and guessing on a 2 GB box:

```go
	if obsProvider.Enabled {
		if runtimeErr := otelruntime.Start(otelruntime.WithMeterProvider(obsProvider.Meter)); runtimeErr != nil {
			zlog.Warn().Err(runtimeErr).Msg("runtime metrics unavailable")
		}
	}
```

(import `otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"`)

Pass metrics into both constructors:
```go
	svc := service.NewGame(redisStore, pgStore, service.WithMetrics(metrics))
```
and add `api.WithMetrics(metrics)` to the existing `api.NewHandler(...)` option list.

Add telemetry state to the existing startup diagnostics log so a misconfigured endpoint is visible at boot rather than discovered by its absence in Grafana:
```go
		Bool("telemetryEnabled", obsProvider.Enabled).
		Str("otlpEndpoint", obsCfg.Endpoint).
		Float64("traceSampleRatio", obsCfg.SampleRatio).
```

Tag each route with its pattern so `http.route` is a low-cardinality label rather than absent. Replace each `mux.HandleFunc("GET /games", handler.ListGamesHandler)`-style registration with the `otelhttp.WithRouteTag` form, keeping the existing middleware chains intact:

```go
	route := func(pattern string, h http.Handler) {
		mux.Handle(pattern, otelhttp.WithRouteTag(pattern, h))
	}

	route("GET /games", http.HandlerFunc(handler.ListGamesHandler))
	route("POST /games", handler.RequireAuth(
		handler.RateLimitByUser("creategame", ratelimit.PerHour(10))(
			http.HandlerFunc(handler.CreateGameHandler))))
	route("POST /games/{id}/join", http.HandlerFunc(handler.JoinGameHandler))
	route("POST /games/{id}/move", http.HandlerFunc(handler.MoveHandler))
	route("GET /games/{id}", http.HandlerFunc(handler.GetGameHandler))
	route("GET /games/{id}/ws", http.HandlerFunc(handler.WSHandler))
	route("GET /lobby/ws", http.HandlerFunc(handler.LobbyWSHandler))
	route("GET /healthz", http.HandlerFunc(api.HealthzHandler))
```

Wrap the outermost handler, preserving the existing chain order and the comment above `srv`:

```go
	var rootHandler http.Handler = handler.LoggingMiddleware(api.BodyLimitMiddleware(mux))

	rootHandler = otelhttp.NewHandler(rootHandler, "mighty",
		otelhttp.WithTracerProvider(obsProvider.Tracer),
		otelhttp.WithMeterProvider(obsProvider.Meter),
		otelhttp.WithFilter(api.TraceFilter))
```

and set `Handler: rootHandler` on the `http.Server`.

- [ ] **Step 9: Verify the build and the whole suite**

Run: `go build ./... && go test ./... && golangci-lint run`
Expected: builds, all tests PASS, lint clean.

Then run the integration suite, which exercises the real WS paths: `go test -v -tags=integration ./tests/e2e/...`
Expected: PASS. **This is the check that proves telemetry is genuinely no-op when unconfigured** — the suite sets no `OTEL_EXPORTER_OTLP_ENDPOINT`.

- [ ] **Step 10: Point the app at Alloy in production**

Add to the environment the app receives — in `remote-deploy.sh`, alongside the existing `.env` writes:

```
OTEL_EXPORTER_OTLP_ENDPOINT=alloy:4317
OTEL_TRACES_SAMPLER_ARG=1
MIGHTY_ENV=prod
```

Add `depends_on: [alloy]` to the `mighty` service in `docker-compose.prod.yml` so the collector is listening before the app starts exporting.

- [ ] **Step 11: Add the opt-in local dev profile**

The dev stack stays telemetry-free by default — that is what makes `go test ./...` and `docker compose up` fast and offline. But the spec calls for an opt-in path so instrumentation can be exercised locally against the same stack, tagged `env=dev`.

Add to `docker-compose.yml` (the dev file), reusing the prod Alloy config:

```yaml
  # Opt-in only: `docker compose --profile observability up`. Ships to the same
  # Grafana Cloud stack tagged env=dev, which is cheaper than standing up local
  # Tempo + Loki + Grafana and lets dashboards filter on the label.
  alloy:
    profiles: ["observability"]
    image: grafana/alloy:v1.10.0
    command:
      - run
      - --server.http.listen-addr=127.0.0.1:12345
      - --storage.path=/var/lib/alloy/data
      - /etc/alloy/config.alloy
    environment:
      MIGHTY_ENV: "dev"
      POSTGRES_CONN: "postgres://postgres:mightypassword@postgres:5432/postgres?sslmode=disable"
      GRAFANA_OTLP_ENDPOINT: "${GRAFANA_OTLP_ENDPOINT:-}"
      GRAFANA_OTLP_INSTANCE_ID: "${GRAFANA_OTLP_INSTANCE_ID:-}"
      GRAFANA_PROM_URL: "${GRAFANA_PROM_URL:-}"
      GRAFANA_PROM_USER_ID: "${GRAFANA_PROM_USER_ID:-}"
      GRAFANA_LOKI_URL: "${GRAFANA_LOKI_URL:-}"
      GRAFANA_LOKI_USER_ID: "${GRAFANA_LOKI_USER_ID:-}"
      GRAFANA_TOKEN: "${GRAFANA_TOKEN:-}"
    volumes:
      - ../deploy/compose/alloy/config.alloy:/etc/alloy/config.alloy:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /:/rootfs:ro
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
```

Fix the relative path to `config.alloy` to whatever is correct from the repo root, where `docker-compose.yml` lives — it is `./deploy/compose/alloy/config.alloy`, not the `../` shown above if the compose file is at the root. Verify with `docker compose --profile observability config` before committing.

Add `OTEL_EXPORTER_OTLP_ENDPOINT: "${OTEL_EXPORTER_OTLP_ENDPOINT:-}"` to the existing `mighty` service's `environment` block. **Empty by default** — so the default `docker compose up` remains fully uninstrumented, and a developer opts in by exporting the variable alongside the Grafana credentials.

Verify both directions:
```bash
docker compose up -d && docker compose logs mighty | grep telemetryEnabled
# Expected: telemetryEnabled=false

OTEL_EXPORTER_OTLP_ENDPOINT=alloy:4317 docker compose --profile observability up -d
docker compose logs mighty | grep telemetryEnabled
# Expected: telemetryEnabled=true
```

- [ ] **Step 12: Build, deploy, and verify app metrics land**

```bash
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin 711387141487.dkr.ecr.us-east-1.amazonaws.com
docker buildx build --platform linux/arm64 -t 711387141487.dkr.ecr.us-east-1.amazonaws.com/mighty:latest --push .
cd deploy/scripts && ./deploy.sh
```

Then confirm the startup log shows `telemetryEnabled=true` and `otlpEndpoint=alloy:4317`:
```bash
INSTANCE=$(cd deploy/terraform && tofu output -raw instance_id)
aws ssm send-command --instance-ids "$INSTANCE" --document-name AWS-RunShellScript \
  --parameters 'commands=["docker logs --tail 30 $(docker ps -qf name=mighty-mighty)"]' \
  --query 'Command.CommandId' --output text
```

In **Explore → prom**, after ~2 minutes:
- `mighty_ws_handshake_total` → present once a client connects (open the app, or `wscat` to the API)
- `http_server_request_duration_seconds_count` → present, with an `http_route` label and **no** raw-path label
- `process_runtime_go_goroutines` → present

**Then verify the cardinality rule held in production**, which is the check that protects the free tier:
```promql
count(count by (__name__) ({__name__=~"mighty.*"}))
```
Expected: a small number (well under 100). Also confirm no `mighty_*` metric carries a `game_id` or `user_id` label by inspecting one series' labels in Explore.

- [ ] **Step 13: Commit**

```bash
git add go.mod go.sum internal/api/hardening.go internal/api/otelhttp_test.go \
        internal/api/ratelimit_middleware.go internal/service/game_service.go \
        cmd/server/main.go deploy/compose/remote-deploy.sh \
        deploy/compose/docker-compose.prod.yml docker-compose.yml
git commit -m "feat(obs): add HTTP, rate-limit and engine metrics; wire OTel into main"
```

---

### Task 10: Service and engine dashboards, Stage-3 alerts

**Files:**
- Create: `deploy/grafana/dashboards/mighty-service-health.json`
- Create: `deploy/grafana/dashboards/mighty-box.json`
- Create: `deploy/grafana/dashboards/mighty-game-engine.json`
- Modify: `deploy/terraform/grafana.tf`

**Interfaces:**
- Consumes: metrics from Tasks 7–9; `grafana_folder.mighty` from Task 3.
- Produces: `grafana_rule_group.warning`; two more entries in `local.critical_rules`.

- [ ] **Step 1: Build the three dashboards in the Grafana UI**

Create each in the **Mighty** folder. Panels and their queries:

**mighty-service-health**
| Panel | Query |
|---|---|
| Request rate by route | `sum by (http_route) (rate(http_server_request_duration_seconds_count[5m]))` |
| Error rate by status | `sum by (http_response_status_code) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"[45].."}[5m]))` |
| p95 latency by route | `histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_request_duration_seconds_bucket[5m])))` |
| Active WS connections | `sum by (kind) (mighty_ws_connections_active)` |
| Handshake outcomes | `sum by (outcome) (rate(mighty_ws_handshake_total[5m]))` |
| WS frame outcomes | `sum by (outcome) (rate(mighty_ws_messages_total[5m]))` |
| Rate-limit rejections | `sum by (bucket) (rate(mighty_ratelimit_rejections_total[5m]))` |
| Goroutines | `process_runtime_go_goroutines` |
| Go heap in use | `process_runtime_go_mem_heap_inuse_bytes` |

**mighty-box**
| Panel | Query |
|---|---|
| CPU busy % | `100 - (avg(rate(node_cpu_seconds_total{job="mighty/host",mode="idle"}[5m])) * 100)` |
| Memory available | `node_memory_MemAvailable_bytes{job="mighty/host"}` |
| Swap used | `node_memory_SwapTotal_bytes{job="mighty/host"} - node_memory_SwapFree_bytes{job="mighty/host"}` |
| Root disk free % | `node_filesystem_avail_bytes{job="mighty/host",mountpoint="/rootfs"} / node_filesystem_size_bytes{job="mighty/host",mountpoint="/rootfs"} * 100` |
| Disk IO | `sum by (device) (rate(node_disk_io_time_seconds_total{job="mighty/host"}[5m]))` |
| Network throughput | `sum by (device) (rate(node_network_receive_bytes_total{job="mighty/host"}[5m]))` |

Add a **swap used** panel deliberately: on a 2 GB box with 2 GB of swap, sustained swap use is the difference between a healthy service and one dying slowly.

**mighty-game-engine**
| Panel | Query |
|---|---|
| Games created | `rate(mighty_games_created_total[15m])` |
| Move outcomes | `sum by (outcome) (rate(mighty_moves_total[5m]))` |
| p95 move latency | `histogram_quantile(0.95, sum by (le) (rate(mighty_move_duration_seconds_bucket[5m])))` |
| Optimistic conflicts | `rate(mighty_optimistic_conflicts_total[10m])` |

Add a text panel to `mighty-game-engine` recording the known gap: *"Active and completed game counts are not shown — `UpdateGameStatus` is never called, so `games.status` in Postgres is frozen at creation. See docs/OBSERVABILITY.md."* A dashboard that silently omits a metric invites someone to assume it is zero.

- [ ] **Step 2: Export each dashboard to the repo**

For each: **Dashboard settings → JSON Model**, copy, and save to the matching `deploy/grafana/dashboards/*.json`. Strip the `id` field (it is stack-specific) and keep `uid`.

- [ ] **Step 3: Add the two remaining critical rules**

Add to `local.critical_rules` in `deploy/terraform/grafana.tf`:

```hcl
    service_down = {
      title   = "Service down - no app telemetry"
      expr    = "absent(mighty_ws_handshake_total) * absent(http_server_request_duration_seconds_count)"
      for     = "2m"
      summary = "No metrics from the mighty process for 2m. Caddy may still be answering, so the Route 53 probe can miss this. Check `docker ps` and app logs."
    }

    http_5xx = {
      title   = "5xx responses"
      expr    = "sum(increase(http_server_request_duration_seconds_count{http_response_status_code=~\"5..\"}[5m])) > bool 5"
      for     = "0m"
      summary = "More than five 5xx responses in 5m. Absolute threshold, not a ratio: at this traffic level a ratio would fire on a single failed request."
    }
```

`absent(a) * absent(b)` is `1` only when **both** are missing, which avoids firing during a quiet period where one instrument has simply not been touched yet.

- [ ] **Step 4: Add the warning rule group**

Append to `deploy/terraform/grafana.tf`. Same `dynamic "rule"` shape as the critical group, with `severity = "warning"` and — unlike the critical group — `no_data_state = "OK"`, because a quiet game with no traffic legitimately produces no data for these and should not page.

```hcl
locals {
  warning_rules = {
    ws_handshake_failures = {
      title   = "WebSocket handshake failures"
      expr    = "(sum(rate(mighty_ws_handshake_total{outcome!=\"ok\"}[15m])) / sum(rate(mighty_ws_handshake_total[15m])) > bool 0.3) * (sum(increase(mighty_ws_handshake_total[15m])) > bool 10)"
      for     = "15m"
      summary = "Over 30% of handshakes failing. Check the `outcome` breakdown: a spike in origin_rejected is almost certainly a bad ALLOWED_ORIGINS deploy."
    }

    latency_degraded = {
      title   = "Request latency degraded"
      expr    = "(histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket[15m]))) > bool 1) * (sum(increase(http_server_request_duration_seconds_count[15m])) > bool 10)"
      for     = "15m"
      summary = "p95 request latency above 1s. Check the Postgres and Redis dashboards for saturation."
    }

    optimistic_conflicts = {
      title   = "Optimistic conflict spike"
      expr    = "sum(rate(mighty_optimistic_conflicts_total[10m])) * 60 > bool 1"
      for     = "10m"
      summary = "Over one version conflict per minute. THRESHOLD IS A GUESS - no baseline existed when it was written. Re-tune against real data (see Task 12)."
    }
  }
}

resource "grafana_rule_group" "warning" {
  name             = "mighty-warning"
  folder_uid       = grafana_folder.mighty.uid
  interval_seconds = 60

  dynamic "rule" {
    for_each = local.warning_rules

    content {
      name      = rule.value.title
      for       = rule.value.for
      condition = "B"

      # Unlike the critical group: a quiet game legitimately produces no data
      # for these, and that is not a problem worth a notification.
      no_data_state  = "OK"
      exec_err_state = "Alerting"

      annotations = {
        summary = rule.value.summary
      }

      labels = {
        severity = "warning"
      }

      data {
        ref_id         = "A"
        datasource_uid = var.prom_datasource_uid

        relative_time_range {
          from = 900
          to   = 0
        }

        model = jsonencode({
          refId         = "A"
          expr          = rule.value.expr
          instant       = true
          intervalMs    = 60000
          maxDataPoints = 43200
        })
      }

      data {
        ref_id         = "B"
        datasource_uid = "__expr__"

        relative_time_range {
          from = 0
          to   = 0
        }

        model = jsonencode({
          refId      = "B"
          type       = "threshold"
          expression = "A"
          conditions = [{
            evaluator = {
              type   = "gt"
              params = [0]
            }
          }]
        })
      }
    }
  }
}
```

Each expression multiplies the condition by a minimum-volume guard so a single request cannot trip a percentile or a ratio.

- [ ] **Step 5: Apply and verify every rule evaluates**

Run: `cd deploy/terraform && tofu apply`

Then in **Alerting → Alert rules**, check all **ten** rules. Every one must be `Normal` or `Pending` — **none may be `Error`**. An `Error` means the PromQL is invalid; paste that rule's expression into Explore to find out why. Do not finish this task with a rule in `Error`: a broken rule is silence that looks like health.

- [ ] **Step 6: Commit**

```bash
git add deploy/grafana/dashboards/ deploy/terraform/grafana.tf
git commit -m "feat(obs): add service/box/engine dashboards and app-level alert rules"
```

---

## Stage 4 — Traces

### Task 11: Database, Redis, and WebSocket message tracing

**Files:**
- Modify: `internal/store/postgres/postgres.go`
- Modify: `internal/store/redis/redis.go`
- Create: `internal/obs/trace.go`
- Modify: `internal/api/ws.go`
- Create: `internal/api/ws_trace_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `obs.Provider` from Task 5.
- Produces: `func obs.Tracer() trace.Tracer`; `func obs.StartWSMessageSpan(ctx context.Context, msgType, gameID, userID, connID string) (context.Context, trace.Span)`

- [ ] **Step 1: Add the tracing dependencies**

```bash
go get github.com/XSAM/otelsql@latest \
       github.com/redis/go-redis/extra/redisotel/v9@latest
go mod tidy
```

- [ ] **Step 2: Write the failing WS span test**

Create `internal/api/ws_trace_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/joekhosbayar/go-mighty/internal/obs"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A per-message span, not a per-connection one: a connection span would stay
// open for an entire game, which is useless in Tempo and a leak in the SDK.
// game_id and user_id belong here, on the span - never on a metric label.
func TestWSMessageSpanCarriesIdentifiers(t *testing.T) {
	t.Parallel()

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := obs.StartWSMessageSpanWithTracer(context.Background(),
		tp.Tracer("test"), "MOVE", "game-1", "user-1", "conn-1")
	require.NotNil(t, ctx)
	span.End()

	ended := rec.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "ws.message MOVE", ended[0].Name())

	attrs := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}

	require.Equal(t, "game-1", attrs["game_id"])
	require.Equal(t, "user-1", attrs["user_id"])
	require.Equal(t, "conn-1", attrs["conn_id"])
}

// The handshake span must close with an error status on every rejection path,
// so handshake failures show up in Tempo's error view without a query.
func TestWSHandshakeSpanRecordsOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{
		obs.OutcomeOK,
		obs.OutcomeAuthTimeout,
		obs.OutcomeAuthFailed,
		obs.OutcomeAuthUnavailable,
		obs.OutcomeOriginRejected,
		obs.OutcomeUpgradeFailed,
		obs.OutcomeConnLimitUser,
		obs.OutcomeConnLimitIP,
	} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()

			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

			_, span := tp.Tracer("test").Start(context.Background(), "ws.handshake game")
			obs.EndWSHandshakeSpan(span, outcome)

			ended := rec.Ended()
			require.Len(t, ended, 1)

			attrs := map[string]string{}
			for _, kv := range ended[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value.AsString()
			}

			require.Equal(t, outcome, attrs["outcome"])

			if outcome == obs.OutcomeOK {
				require.Equal(t, codes.Unset, ended[0].Status().Code)
			} else {
				require.Equal(t, codes.Error, ended[0].Status().Code)
			}
		})
	}
}
```

Imports for this file: `context`, `testing`, `github.com/stretchr/testify/require`, `github.com/joekhosbayar/go-mighty/internal/obs`, `go.opentelemetry.io/otel/codes`, `sdktrace "go.opentelemetry.io/otel/sdk/trace"`, `go.opentelemetry.io/otel/sdk/trace/tracetest`.

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/api/ -run 'TestWSMessageSpan|TestWSHandshakeSpan' -v`
Expected: FAIL — `undefined: obs.StartWSMessageSpanWithTracer`, `undefined: obs.EndWSHandshakeSpan`.

- [ ] **Step 4: Implement `internal/obs/trace.go`**

```go
package obs

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/joekhosbayar/go-mighty"

// Tracer returns the service tracer from the globally installed provider,
// which is a no-op provider unless Init enabled exporting.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// StartWSMessageSpan starts a span for one inbound WebSocket frame.
//
// This is deliberately a NEW ROOT span per message rather than a child of a
// connection-scoped span: a connection span would stay open for an entire
// game, which is unusable in Tempo and holds SDK memory for hours.
//
// game_id, user_id and conn_id are span attributes. They must never become
// metric labels - see the cardinality guard in metrics_test.go.
func StartWSMessageSpan(ctx context.Context, msgType, gameID, userID, connID string) (context.Context, trace.Span) {
	return StartWSMessageSpanWithTracer(ctx, Tracer(), msgType, gameID, userID, connID)
}

// StartWSMessageSpanWithTracer is StartWSMessageSpan with an explicit tracer,
// so tests can record spans without touching global state.
func StartWSMessageSpanWithTracer(
	ctx context.Context,
	tracer trace.Tracer,
	msgType, gameID, userID, connID string,
) (context.Context, trace.Span) {
	return tracer.Start(ctx, "ws.message "+msgType,
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("game_id", gameID),
			attribute.String("user_id", userID),
			attribute.String("conn_id", connID),
			attribute.String("ws.message_type", msgType),
		))
}

// StartWSHandshakeSpan starts a short-lived span covering upgrade, AUTH, and
// the accept/reject decision — the part of a WebSocket's life that actually
// fails. It deliberately does NOT span the connection: see
// StartWSMessageSpan for why.
//
// The caller must call EndWSHandshakeSpan on every exit path.
func StartWSHandshakeSpan(ctx context.Context, kind string) (context.Context, trace.Span) {
	return Tracer().Start(ctx, "ws.handshake "+kind,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("kind", kind)))
}

// EndWSHandshakeSpan records the handshake outcome and closes the span. Any
// outcome other than OutcomeOK is marked as an error status so handshake
// failures surface in Tempo's error view without a query.
func EndWSHandshakeSpan(span trace.Span, outcome string) {
	span.SetAttributes(attribute.String("outcome", outcome))

	if outcome != OutcomeOK {
		span.SetStatus(codes.Error, outcome)
	}

	span.End()
}
```

(`codes` is `go.opentelemetry.io/otel/codes`.)

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/api/ -run 'TestWSMessageSpan|TestWSHandshakeSpan' -v`
Expected: PASS, including all eight handshake-outcome subtests.

- [ ] **Step 6: Wrap the handshake in its own span**

The spec calls for a handshake span alongside the handshake metric from Task 8. Both cover the same branches, so wire them together rather than duplicating the branch analysis.

In `internal/api/ws.go`, immediately after `gameID := r.PathValue("id")` and before `up := h.upgrader()`:

```go
	hsCtx, hsSpan := obs.StartWSHandshakeSpan(r.Context(), obs.KindGame)
```

Then, at each of the branches instrumented in Task 8, add the matching span close **next to** the existing `RecordHandshake` call, passing the same outcome constant. For example, in the auth-timeout branch:

```go
			h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeAuthTimeout)
			obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeAuthTimeout)
```

Switch every `RecordHandshake(r.Context(), ...)` added in Task 8 to `RecordHandshake(hsCtx, ...)` so the metric and span share a context.

At the success point — where Task 8 records `OutcomeOK` and increments the connection gauge — end the handshake span there too:

```go
	obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeOK)
```

**End the span at the success point, not with a `defer`.** A deferred end would keep it open for the whole game, which is exactly the shape the spec rejected. Every branch that returns before success must end it explicitly; `golangci-lint` will not catch a missed one, so walk the branch list from Task 8 Step 4 and confirm all nine are covered.

The `origin_rejected` case is the exception: `checkOrigin` runs inside `up.Upgrade`, before `hsSpan` could be threaded in cleanly. Leave that one recording only its metric, and record the span outcome as `OutcomeUpgradeFailed` in the post-upgrade error branch — the metric already distinguishes the two causes.

Apply the same treatment to `lobby_ws.go` with `obs.KindLobby`.

- [ ] **Step 7: Use the span in the WS read loop**

In `internal/api/ws.go`:

Generate a connection id once, immediately after a successful upgrade, so log lines and spans for one socket can be correlated:
```go
	connID := uuid.NewString()
```
(`github.com/google/uuid` is already a direct dependency.)

In the read loop, after a frame successfully unmarshals into `inMsg` and is about to be handled, wrap the handling:
```go
		msgCtx, msgSpan := obs.StartWSMessageSpan(r.Context(), inMsg.Type, gameID, claims.UserID, connID)
```
Pass `msgCtx` — not `r.Context()` — to `h.svc.ProcessMove`, so the Redis and Postgres spans created inside nest under it. On the `ProcessMove` error path call `msgSpan.RecordError(err)` and `msgSpan.SetStatus(codes.Error, err.Error())` before `continue`. End the span on every exit path from that iteration (a small closure or an explicit `msgSpan.End()` before each `continue`/loop end — **not** a `defer`, which inside a loop would hold every span until the socket closes).

Switch the log calls inside this loop from `log.Warn()`/`log.Error()` to `obs.Log(msgCtx).Warn()`/`.Error()` so those lines carry `trace_id`.

- [ ] **Step 8: Instrument Postgres with otelsql**

In `internal/store/postgres/postgres.go`, replace `sql.Open("postgres", connStr)` inside `NewStore`:

```go
	db, err := otelsql.Open("postgres", connStr,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			// One span per query is enough; a span per Rows.Next would
			// multiply trace volume by the row count for no diagnostic gain.
			DisableErrSkip: true,
			OmitRows:       true,
		}))
	if err != nil {
		return nil, err
	}

	// Pool stats: open, idle, and crucially WAITING connections, which are
	// the earliest honest signal of pool pressure and back the Postgres
	// saturation alert.
	if err := otelsql.RegisterDBStatsMetrics(db,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL)); err != nil {
		return nil, err
	}
```

`NewStoreWithDB` is left untouched — it takes an already-open `*sql.DB` and is used by the `go-sqlmock` tests.

- [ ] **Step 9: Instrument Redis**

In `internal/store/redis/redis.go`, in `NewStore` after the client is created:

```go
	// Errors here mean the hooks could not attach; the store is still usable,
	// so degrade to uninstrumented rather than failing to boot. NewStore has
	// no error return and adding one would churn every call site.
	if err := redisotel.InstrumentTracing(client); err != nil {
		log.Warn().Err(err).Msg("redis tracing unavailable")
	}

	if err := redisotel.InstrumentMetrics(client); err != nil {
		log.Warn().Err(err).Msg("redis metrics unavailable")
	}
```

Apply the same two calls to the separate rate-limiter client created in `cmd/server/main.go` (`rlClient`).

- [ ] **Step 10: Verify the full suite, including integration**

Run: `go build ./... && go test ./... && golangci-lint run`
Expected: PASS, clean.

Run: `go test -v -tags=integration ./tests/e2e/...`
Expected: PASS. The `miniredis` and `go-sqlmock` based tests must be unaffected — if any now fails, an instrumentation hook is being attached to a mock that does not support it.

- [ ] **Step 11: Deploy and verify traces arrive**

```bash
docker buildx build --platform linux/arm64 -t 711387141487.dkr.ecr.us-east-1.amazonaws.com/mighty:latest --push .
cd deploy/scripts && ./deploy.sh
```

Exercise the API (create a game, join, make a move over WebSocket). Then in **Explore → grafanacloud-\<stack\>-traces**, search service `mighty`.

Expected:
- an HTTP span such as `POST /games` with nested `pg.query` spans
- a `ws.message MOVE` **root** span with nested Redis and Postgres spans
- **no** span covering a whole WebSocket connection
- `game_id` / `user_id` present as span attributes

- [ ] **Step 12: Link logs to traces**

In **Connections → Data sources → grafanacloud-\<stack\>-logs → Derived fields**, add:

| Field | Value |
|---|---|
| Name | `trace_id` |
| Type | Regex in log line |
| Regex | `"trace_id":"(\w+)"` |
| Query | `${__value.raw}` |
| Internal link | the `grafanacloud-<stack>-traces` datasource |

Verify: in **Explore → logs**, query `{service="mighty", container="mighty"} | json | trace_id != ""`, expand a line, and click the `trace_id` link.
Expected: the matching trace opens in Tempo. This is the whole payoff of the zerolog work in Task 6 — if the link does not resolve, the regex does not match the actual field format, so check a raw log line.

- [ ] **Step 13: Commit**

```bash
git add go.mod go.sum internal/obs/trace.go internal/api/ws.go internal/api/ws_trace_test.go \
        internal/store/postgres/postgres.go internal/store/redis/redis.go cmd/server/main.go
git commit -m "feat(obs): trace SQL, Redis and WebSocket messages"
```

---

## Stage 5 — Measure, decide, document

### Task 12: Headroom measurement, cadvisor decision, and the runbook

**Files:**
- Modify: `deploy/compose/alloy/config.alloy` (conditionally)
- Modify: `deploy/terraform/grafana.tf`
- Create: `docs/OBSERVABILITY.md`

**Interfaces:**
- Consumes: everything from Tasks 1–11.
- Produces: `docs/OBSERVABILITY.md`; a decision, recorded either way, on container metrics.

- [ ] **Step 1: Measure actual memory headroom on the box**

This is the number the spec explicitly refused to estimate.

```bash
INSTANCE=$(cd deploy/terraform && tofu output -raw instance_id)
CMD=$(aws ssm send-command --instance-ids "$INSTANCE" --document-name AWS-RunShellScript \
  --parameters 'commands=["free -m","docker stats --no-stream --format \"table {{.Name}}\\t{{.MemUsage}}\\t{{.MemPerc}}\"","cat /proc/meminfo | grep -E \"MemAvailable|SwapFree|SwapTotal\""]' \
  --query 'Command.CommandId' --output text)
sleep 5
aws ssm get-command-invocation --command-id "$CMD" --instance-id "$INSTANCE" \
  --query StandardOutputContent --output text
```

Record three numbers: **Alloy's RSS**, **`MemAvailable`**, and **swap used**.

- [ ] **Step 2: Decide on container metrics using the recorded gate**

The spec's gate: **if `MemAvailable` is under ~200 MB, cadvisor stays off** and that is written down as a known gap.

*If headroom is ≥ 200 MB*, append to `config.alloy`:

```alloy
// ------------------------------------------------------------ container metrics
// The heaviest component here by a wide margin, which is why it was enabled
// only after measuring headroom. enabled_metrics is restricted deliberately:
// the default set is several hundred series per container.

prometheus.exporter.cadvisor "containers" {
  docker_host = "unix:///var/run/docker.sock"

  enabled_metrics = ["cpu", "memory", "diskIO", "network"]

  storage_duration = "1m"
}

discovery.relabel "cadvisor" {
  targets = prometheus.exporter.cadvisor.containers.targets

  rule {
    target_label = "job"
    replacement  = "mighty/containers"
  }

  rule {
    target_label = "env"
    replacement  = sys.env("MIGHTY_ENV")
  }
}

prometheus.scrape "cadvisor" {
  targets         = discovery.relabel.cadvisor.output
  forward_to      = [prometheus.remote_write.grafana_cloud.receiver]
  scrape_interval = "60s"
}
```

Then validate with `alloy fmt`, deploy, **re-run Step 1**, and confirm headroom is still above 150 MB (the memory alert threshold). If it is not, revert this step — a monitoring component that trips your own memory alert is a net loss.

Also add the crash-loop rule that cadvisor makes possible, to `local.critical_rules`:

```hcl
    container_restarting = {
      title   = "Container restart loop"
      expr    = "sum(increase(container_start_time_seconds{job=\"mighty/containers\"}[15m]) > bool 0) > bool 1"
      for     = "5m"
      summary = "A container has restarted more than once in 15m. Check `docker ps` and the container's logs."
    }
```

*If headroom is under 200 MB*, skip all of the above and record the gap in Step 4.

- [ ] **Step 3: Add the free-tier ceiling alert**

Grafana Cloud publishes usage metrics into the stack's own Prometheus. Confirm the exact metric name first in **Explore → prom** with `{__name__=~"grafanacloud_org_metrics.*"}`, then add to `local.critical_rules`:

```hcl
    free_tier_ceiling = {
      title   = "Approaching free-tier series limit"
      expr    = "max(grafanacloud_org_metrics_billable_series) > bool 8000"
      for     = "30m"
      summary = "Over 8k of 10k billable series. Past the cap, data is silently dropped - exactly when you would need it. Find the offender with: topk(10, count by (__name__) ({__name__=~\".+\"}))."
    }
```

If the metric name differs in your stack, use the one Explore reports rather than this literal.

Apply: `cd deploy/terraform && tofu apply`, then confirm in **Alerting → Alert rules** that the new rules are `Normal`, not `Error`.

- [ ] **Step 4: Check trace and log volume against the ceilings**

The spec flagged a runaway WS message loop as the plausible route to the 50 GB trace ceiling.

In **Explore → prom**, check the stack's ingest usage metrics (`grafanacloud_org_traces_*`, `grafanacloud_org_logs_*`). Project the current daily rate to 30 days.

If traces project past ~40 GB/month, lower sampling — set `OTEL_TRACES_SAMPLER_ARG=0.1` in `remote-deploy.sh` and redeploy. This needs no code change, which is why Task 5 made the ratio env-tunable.

Record the observed rates in the runbook.

- [ ] **Step 5: Write the runbook**

Create `docs/OBSERVABILITY.md` covering:

1. **What is collected** — the three pillars, their destinations, the 60s interval.
2. **How to get credentials** — the seven SSM parameters, that they are written out-of-band, and how to rotate the access-policy token.
3. **The cardinality rule** — and that `internal/obs/metrics_test.go` enforces it. Anyone adding a metric must read this.
4. **The ten (or twelve) alert rules** — what each means and the first thing to check for each. Copy the `summary` annotations so the runbook and the alerts cannot drift apart.
5. **Dashboards** — that JSON in `deploy/grafana/dashboards/` is an exported artifact, not the source of truth; edit in the UI, re-export, commit.
6. **Known gaps**, explicitly:
   - `mighty.games.active` and `mighty.games.completed` do not exist. `postgres.Store.UpdateGameStatus` is never called, so `games.status` is frozen at creation and there is no trustworthy source. **This also means `ListGamesByStatus` — which backs `GET /games` for the lobby — reads stale status. That is a pre-existing product bug, not an observability one, and it is unfixed.**
   - Container metrics and the crash-loop alert: enabled or not, with the measured headroom number that decided it.
   - The optimistic-conflict threshold is a guess pending a real baseline.
   - Frontend telemetry (web, Electron, Swift) is out of scope.
   - `terraform.tfvars` holds the Grafana service-account token and Discord webhook in cleartext on the operator's laptop, alongside the pre-existing GitHub PAT. Gitignored, so not in version control, but not encrypted either.
7. **The measured numbers** from Steps 1 and 4, dated, so the next person can tell drift from noise.

- [ ] **Step 6: Verify the whole thing once more, end to end**

- `go test ./... && golangci-lint run` → PASS, clean
- `go test -v -tags=integration ./tests/e2e/...` → PASS
- **Alerting → Alert rules**: every rule `Normal` or `Pending`, none `Error`
- All three custom dashboards populate with data
- One log line's `trace_id` link opens its trace in Tempo
- **Trip one alert for real one final time** (as in Task 3 Step 6) and confirm Discord delivery, then revert. The path was verified before most of these rules existed; verify it again now that they all do.

- [ ] **Step 7: Commit**

```bash
git add docs/OBSERVABILITY.md deploy/compose/alloy/config.alloy deploy/terraform/grafana.tf
git commit -m "docs(obs): add observability runbook; finalise container metrics decision"
```

---

## Verification Summary

Every claim this plan makes about production is backed by a specific check:

| Claim | Verified by |
|---|---|
| Telemetry is no-op when unconfigured | Task 5 Step 5 (goleak), Task 9 Step 9 (integration suite with no endpoint set), Task 9 Step 11 (dev profile off by default) |
| WebSockets still work through the middleware chain | Task 9 Step 2/5 (regression test), Task 11 Step 11 (real move over WS) |
| The cardinality rule holds | Task 7 Step 5 (guard test fails when violated), Task 9 Step 12 (series count in prod) |
| Alloy fits in the memory budget | Task 12 Step 1 (measured RSS and `MemAvailable`) |
| Alerts actually reach Discord | Task 3 Step 5/6, re-verified Task 12 Step 6 |
| No alert rule is silently broken | Task 4 Step 7, Task 10 Step 5, Task 12 Steps 3/6 (no rule in `Error`) |
| Handshake spans carry outcomes and error status | Task 11 Step 2 (`TestWSHandshakeSpanRecordsOutcome`) |
| Logs link to traces | Task 11 Step 12 (click through to Tempo) |
| Free-tier ceilings are not being approached | Task 12 Steps 3–4 |
| The log feedback loop is excluded | Task 2 Step 8 (`{container=~".*alloy.*"}` returns nothing) |
