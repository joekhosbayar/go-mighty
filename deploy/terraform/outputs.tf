output "ecr_repo_url" {
  value = aws_ecr_repository.mighty.repository_url
}

output "deploy_bucket" {
  value = aws_s3_bucket.deploy.bucket
}

output "name_servers" {
  value = aws_route53_zone.main.name_servers
}

output "instance_id" {
  value = aws_instance.api.id
}

output "public_ip" {
  value = aws_eip.api.public_ip
}

output "cognito_pool_id" {
  value = aws_cognito_user_pool.main.id
}

output "cognito_client_id" {
  value = aws_cognito_user_pool_client.spa.id
}

output "caddy_ecr_repo_url" {
  value = aws_ecr_repository.caddy.repository_url
}
