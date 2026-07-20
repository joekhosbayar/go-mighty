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

resource "aws_ssm_parameter" "cognito_pool_id" {
  name  = "/mighty/cognito_pool_id"
  type  = "String"
  value = aws_cognito_user_pool.main.id
}

resource "aws_ssm_parameter" "cognito_client_id" {
  name  = "/mighty/cognito_client_id"
  type  = "String"
  value = aws_cognito_user_pool_client.spa.id
}
