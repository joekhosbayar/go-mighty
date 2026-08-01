#!/bin/bash
set -euo pipefail
export AWS_DEFAULT_REGION=us-east-1
cd /opt/mighty

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
ECR_HOST="${ACCOUNT_ID}.dkr.ecr.us-east-1.amazonaws.com"
ECR_IMAGE="${ECR_HOST}/mighty:latest"

param() {
	aws ssm get-parameter --name "$1" --with-decryption \
		--query Parameter.Value --output text
}

PGPW=$(param /mighty/postgres_password)

umask 077
cat > .env <<EOF
ECR_IMAGE=${ECR_IMAGE}
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
GRAFANA_OTLP_ENDPOINT=$(param /mighty/grafana/otlp_endpoint)
GRAFANA_OTLP_INSTANCE_ID=$(param /mighty/grafana/otlp_instance_id)
GRAFANA_PROM_URL=$(param /mighty/grafana/prom_url)
GRAFANA_PROM_USER_ID=$(param /mighty/grafana/prom_user_id)
GRAFANA_LOKI_URL=$(param /mighty/grafana/loki_url)
GRAFANA_LOKI_USER_ID=$(param /mighty/grafana/loki_user_id)
GRAFANA_TOKEN=$(param /mighty/grafana/token)
OTEL_EXPORTER_OTLP_ENDPOINT=alloy:4317
OTEL_TRACES_SAMPLER_ARG=1
MIGHTY_ENV=prod
EOF

aws ecr get-login-password | docker login --username AWS --password-stdin "$ECR_HOST"
docker compose -f docker-compose.prod.yml pull

# MIGHTY_VERSION becomes service.version on every OTel signal. There is no
# CI pipeline tagging a git SHA (Phase 1 of DEPLOY.md is a manual
# `docker buildx build ... -t mighty:latest --push`), so the tag itself is
# always "latest" and useless as a version. The pulled image digest is the
# best fingerprint actually available on this host: it uniquely identifies
# the image content that is about to run, appended to .env after the pull so
# `up -d` picks it up.
IMAGE_DIGEST=$(docker image inspect "${ECR_IMAGE}" --format '{{index .RepoDigests 0}}' 2>/dev/null | cut -d'@' -f2)
echo "MIGHTY_VERSION=${IMAGE_DIGEST:-unknown}" >> .env

docker compose -f docker-compose.prod.yml up -d --remove-orphans
docker image prune -f
