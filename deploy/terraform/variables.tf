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
