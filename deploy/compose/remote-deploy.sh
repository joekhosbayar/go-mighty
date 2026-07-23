#!/bin/bash
set -euo pipefail
export AWS_DEFAULT_REGION=us-east-1
cd /opt/mighty

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
ECR_HOST="${ACCOUNT_ID}.dkr.ecr.us-east-1.amazonaws.com"

param() {
	aws ssm get-parameter --name "$1" --with-decryption \
		--query Parameter.Value --output text
}

PGPW=$(param /mighty/postgres_password)

umask 077
cat > .env <<EOF
ECR_IMAGE=${ECR_HOST}/mighty:latest
CADDY_IMAGE=${ECR_HOST}/mighty-caddy:latest
POSTGRES_PASSWORD=${PGPW}
POSTGRES_CONN=postgres://postgres:${PGPW}@postgres:5432/postgres?sslmode=disable
COGNITO_POOL_ID=$(param /mighty/cognito_pool_id)
COGNITO_CLIENT_ID=$(param /mighty/cognito_client_id)
COGNITO_REGION=us-east-1
REDIS_ADDR=redis:6379
LOG_LEVEL=info
ALLOWED_ORIGINS=https://themighty.gg,https://www.themighty.gg
TRUST_PROXY_HEADERS=true
API_DOMAIN=$(param /mighty/api_domain)
ACME_EMAIL=$(param /mighty/acme_email)
EOF

aws ecr get-login-password | docker login --username AWS --password-stdin "$ECR_HOST"
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d --remove-orphans
docker image prune -f
