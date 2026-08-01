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

locals {
  # Each expr MUST evaluate to 0 or 1 (use the `bool` modifier on
  # comparisons) so the shared `gt 0` threshold below applies uniformly.
  #
  # Stage 1 only: telemetry_blind, memory_exhausted, disk_filling. Other
  # rules (postgres_saturation, service_down, http_5xx, warning-group rules)
  # are staged to land in a later task, after the metrics backing them
  # exist — creating them now would fire immediately given
  # no_data_state = "Alerting" below.
  #
  # SEQUENCING: Alloy (deploy/compose/alloy) must already be deployed and
  # confirmed scraping `job="mighty/host"` before this stack is `tofu
  # apply`'d. no_data_state = "Alerting" is deliberate — a dead telemetry
  # path should page — but it also means applying this before Alloy is up
  # and scraped at least once pages you for your own install, not a real
  # incident.
  critical_rules = {
    telemetry_blind = {
      title = "Telemetry blind - no host metrics"
      # absent() alone does NOT give a 0/1 series: when metrics are healthy
      # it returns an EMPTY vector (not 0), which Grafana treats as No Data
      # -> Alerting given no_data_state below, so a naive absent() fires in
      # both the healthy and blind states. count(...) * 0 collapses any
      # number of series to a single label-less 0 when data exists; `or
      # vector(1)` only contributes when the left side is empty (no
      # series), giving a single label-less 1 when blind. Exactly one
      # series, 0 or 1, in every state.
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
      title = "Disk filling"
      # mountpoint is "/" here, not "/rootfs": node_exporter's rootfs_path
      # (set to /rootfs in config.alloy so the container can statfs() the
      # host root from under /rootfs) is used only to redirect the syscall
      # target — it is stripped from the exposed `mountpoint` label, which
      # reports the real host path ("/").
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
