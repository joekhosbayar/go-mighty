#!/usr/bin/env bash
# Builds the custom Caddy (rate-limit plugin) for the Graviton instance and
# pushes it to ECR. Run this whenever Dockerfile.caddy changes — the image is
# not rebuilt by the app deploy.
set -euo pipefail
cd "$(dirname "$0")/../.."

REGION=us-east-1
REPO=$(tofu -chdir=deploy/terraform output -raw caddy_ecr_repo_url)
ECR_HOST=${REPO%%/*}

aws ecr get-login-password --region "$REGION" \
	| docker login --username AWS --password-stdin "$ECR_HOST"

docker buildx build \
	--platform linux/arm64 \
	-f deploy/compose/Dockerfile.caddy \
	-t "${REPO}:latest" \
	--push \
	deploy/compose

echo "Pushed ${REPO}:latest"
