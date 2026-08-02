variable "region" {
  type    = string
  default = "us-east-1"
}

variable "domain" {
  type        = string
  description = "Apex domain registered in Route 53, e.g. playmighty.com"
}

variable "alert_email" {
  type        = string
  description = "Email for CloudWatch/Budgets alerts and ACME registration"
}

variable "instance_type" {
  type    = string
  default = "t4g.small"
}

locals {
  api_domain = "api.${var.domain}"
}

variable "github_token" {
  type        = string
  description = "GitHub Personal Access Token for Amplify Hosting"
  sensitive   = true
}

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
