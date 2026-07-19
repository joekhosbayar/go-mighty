output "ecr_repo_url" {
  value = aws_ecr_repository.mighty.repository_url
}

output "deploy_bucket" {
  value = aws_s3_bucket.deploy.bucket
}

output "name_servers" {
  value = aws_route53_zone.main.name_servers
}
