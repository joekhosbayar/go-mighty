resource "aws_ssm_parameter" "api_domain" {
  name  = "/mighty/api_domain"
  type  = "String"
  value = local.api_domain
}

resource "aws_ssm_parameter" "acme_email" {
  name  = "/mighty/acme_email"
  type  = "String"
  value = var.alert_email
}
