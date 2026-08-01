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
  critical_rules = {
    telemetry_blind = {
      title   = "Telemetry blind - no host metrics"
      expr    = "absent(node_memory_MemAvailable_bytes{job=\"mighty/host\"})"
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
      expr    = "min(node_filesystem_avail_bytes{job=\"mighty/host\", mountpoint=\"/rootfs\"} / node_filesystem_size_bytes{job=\"mighty/host\", mountpoint=\"/rootfs\"}) < bool 0.15"
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
