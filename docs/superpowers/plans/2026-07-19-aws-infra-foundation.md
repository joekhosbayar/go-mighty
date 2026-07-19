# AWS Infra Foundation Implementation Plan (Plan 1 of 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy the current go-mighty stack (existing JWT auth, unchanged) to AWS: one EC2 t4g.small running docker compose (Caddy TLS + app + Postgres + Redis), reachable at `https://api.<domain>` with working WSS, plus snapshots, alarms, and a budget tripwire.

**Architecture:** OpenTofu manages all AWS resources (EC2 + cloud-init, SG, IAM, EIP, Route 53, ECR, S3 deploy bucket, DLM, SNS/CloudWatch, Budgets). App deploys as an ARM64 image from ECR via `aws ssm send-command` — no SSH anywhere. Secrets live in SSM Parameter Store and are rendered to `.env` on the instance at deploy time.

**Tech Stack:** OpenTofu ≥ 1.10, AWS CLI v2, Docker buildx, Caddy 2 (stock image — the xcaddy rate-limit build is Plan 4), golang-migrate, Amazon Linux 2023 (ARM64).

**Spec:** `docs/superpowers/specs/2026-07-18-aws-mvp-architecture-design.md` (Sections 1, 3-Layer 3, 4-alerting subset). Out of scope here: Cognito (Plans 2–3), rate limiting (Plan 4), OTel/Grafana + CI/CD + Amplify (Plan 5).

## Global Constraints

- Region: **us-east-1** for every resource and CLI call.
- Compute: **t4g.small (ARM64/Graviton)** — all images must be built/pulled as **linux/arm64**.
- Security group inbound: **443 and 80 only**. **No SSH ever** — admin access via SSM Session Manager only.
- Postgres and Redis: compose network only, **no host port mappings** in prod.
- Secrets: **SSM Parameter Store SecureString** only — never in the repo, never in Terraform state (created via CLI, not TF).
- Budget guard: AWS Budgets **$25 forecast / $30 actual** → email (spec Section 4).
- IaC lives in `deploy/terraform/`; prod runtime files in `deploy/compose/`; operator scripts in `deploy/scripts/`.
- `<domain>` is a placeholder throughout — substitute the real domain bought in Task 0. Same for `<alert-email>` and `<account-id>`.
- All local paths are relative to the `go-mighty` repo root.

---

### Task 0: Manual prerequisites (one-time, operator actions)

**Files:** none (AWS console/CLI only)

**Interfaces:**
- Consumes: nothing
- Produces: authenticated AWS CLI, a Route 53 hosted zone for `<domain>`, an S3 state bucket `mighty-tfstate-<account-id>`, `tofu` installed. Every later task assumes these exist.

- [ ] **Step 1: Verify AWS CLI auth and capture the account ID**

Run: `aws sts get-caller-identity --query Account --output text`
Expected: a 12-digit account ID. If it errors, stop and authenticate first (`aws configure` or `aws login` / SSO).

- [ ] **Step 2: Register the domain (external registrar OK)**

Register `<domain>` at any registrar — Porkbun is fine and cheaper than Route 53 for some TLDs. DNS will be **hosted** in Route 53 regardless (Terraform creates the hosted zone in Task 3); the registrar's only ongoing job is renewals and pointing NS records at AWS (Task 3's delegation step). If registering via Route 53 instead, its auto-created hosted zone must be imported or deleted before Task 3's apply so Terraform owns the zone.

- [ ] **Step 3: Verify the domain is registered and controllable**

Log into the registrar and confirm the domain is active and its nameserver settings are editable. (The Route 53 hosted zone doesn't exist yet — it arrives in Task 3.)

- [ ] **Step 4: Install OpenTofu**

Run: `brew install opentofu && tofu version`
Expected: `OpenTofu v1.10.x` or newer (needed for S3 native state locking).

- [ ] **Step 5: Create the Terraform state bucket (versioned)**

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
aws s3api create-bucket --bucket "mighty-tfstate-${ACCOUNT_ID}" --region us-east-1
aws s3api put-bucket-versioning --bucket "mighty-tfstate-${ACCOUNT_ID}" \
  --versioning-configuration Status=Enabled
```

Expected: no errors; `aws s3api get-bucket-versioning --bucket "mighty-tfstate-${ACCOUNT_ID}"` shows `"Status": "Enabled"`.

---

### Task 1: `/healthz` endpoint (TDD)

**Files:**
- Create: `internal/api/health.go`
- Create: `internal/api/health_test.go`
- Modify: `cmd/server/main.go` (router block, after the `GET /games/{id}/ws` line)

**Interfaces:**
- Consumes: nothing
- Produces: `api.HealthzHandler(w http.ResponseWriter, r *http.Request)` — plain `http.HandlerFunc`; route `GET /healthz` returns `200` body `ok`. Task 7's Route 53 health check and Task 6's smoke test depend on this exact path and status.

- [ ] **Step 1: Write the failing test**

Create `internal/api/health_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	HealthzHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHealthzHandler -v`
Expected: FAIL — `undefined: HealthzHandler` (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/api/health.go`:

```go
package api

import "net/http"

// HealthzHandler is a shallow liveness probe for Route 53 health checks and
// load-path smoke tests. It deliberately checks nothing downstream: a dying
// dependency should page via its own alarms, not flap DNS.
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestHealthzHandler -v`
Expected: PASS.

- [ ] **Step 5: Register the route**

In `cmd/server/main.go`, in the router block (section `// 5. Router`), add after the WebSocket line:

```go
	mux.HandleFunc("GET /healthz", api.HealthzHandler)
```

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: all packages PASS (or same pre-existing state as before this task — no new failures).

- [ ] **Step 7: Commit**

```bash
git add internal/api/health.go internal/api/health_test.go cmd/server/main.go
git commit -m "feat: add /healthz liveness endpoint for external health checks"
```

---

### Task 2: ARM64-capable Docker build + demo script prod support

**Files:**
- Modify: `Dockerfile` (build stage)
- Modify: `scripts/demo_game_flow.sh:9`

**Interfaces:**
- Consumes: nothing
- Produces: `docker buildx build --platform linux/arm64 .` yields a runnable ARM64 image (Task 5 pushes it); `BASE_URL=<url> ./scripts/demo_game_flow.sh` targets any deployment (Task 6 verification).

- [ ] **Step 1: Make the Dockerfile cross-compile to the target platform**

In `Dockerfile`, replace:

```dockerfile
FROM golang:1.25 AS build
```

with:

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETARCH
```

and replace:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o mighty ./cmd/server
```

with:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o mighty ./cmd/server
```

(`TARGETARCH` is auto-populated by BuildKit; `--platform=$BUILDPLATFORM` keeps the compile stage native-speed and cross-compiles via Go instead of QEMU.)

- [ ] **Step 2: Verify an ARM64 image builds and reports the right architecture**

```bash
docker buildx build --platform linux/arm64 -t mighty:arm64-check --load .
docker inspect mighty:arm64-check --format '{{.Architecture}}'
```

Expected: build succeeds; inspect prints `arm64`.

- [ ] **Step 3: Verify the local dev flow still works (amd64/native)**

Run: `docker compose build`
Expected: builds without error (compose still uses the same Dockerfile; `TARGETARCH` defaults to the host arch).

- [ ] **Step 4: Make the demo script's base URL overridable**

In `scripts/demo_game_flow.sh`, replace line 9:

```bash
BASE_URL="http://localhost:8080"
```

with:

```bash
BASE_URL="${BASE_URL:-http://localhost:8080}"
```

- [ ] **Step 5: Commit**

```bash
git add Dockerfile scripts/demo_game_flow.sh
git commit -m "build: cross-compile Docker image for arm64; allow demo script BASE_URL override"
```

---

### Task 3: Terraform scaffold + ECR + deploy bucket + hosted zone + config parameters

**Files:**
- Create: `deploy/terraform/versions.tf`
- Create: `deploy/terraform/variables.tf`
- Create: `deploy/terraform/ecr.tf`
- Create: `deploy/terraform/s3.tf`
- Create: `deploy/terraform/dns.tf`
- Create: `deploy/terraform/ssm.tf`
- Create: `deploy/terraform/outputs.tf`
- Create: `deploy/terraform/terraform.tfvars.example`
- Modify: `.gitignore` (append terraform entries; create the file if absent)

**Interfaces:**
- Consumes: state bucket + externally registered domain from Task 0.
- Produces: tofu workspace that later tasks add files into; resources `aws_ecr_repository.mighty`, `aws_s3_bucket.deploy`, `aws_route53_zone.main` (NS records delegated at the registrar in this task — propagation must complete before Task 6's ACME issuance); SSM params `/mighty/api_domain`, `/mighty/acme_email`; outputs `ecr_repo_url`, `deploy_bucket`, `name_servers`. Variable names `domain`, `alert_email`, `region`, `instance_type` are referenced by every later `.tf` file.

- [ ] **Step 1: Write the scaffold files**

Create `deploy/terraform/versions.tf` (replace `<account-id>` with the real ID — backend blocks cannot use variables):

```hcl
terraform {
  required_version = ">= 1.10"

  backend "s3" {
    bucket       = "mighty-tfstate-<account-id>"
    key          = "mvp/terraform.tfstate"
    region       = "us-east-1"
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = { Project = "mighty" }
  }
}

data "aws_caller_identity" "current" {}
```

Create `deploy/terraform/variables.tf`:

```hcl
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
```

Create `deploy/terraform/ecr.tf`:

```hcl
resource "aws_ecr_repository" "mighty" {
  name = "mighty"

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "mighty" {
  repository = aws_ecr_repository.mighty.name

  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "keep last 10 images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 10
      }
      action = { type = "expire" }
    }]
  })
}
```

Create `deploy/terraform/s3.tf`:

```hcl
resource "aws_s3_bucket" "deploy" {
  bucket = "mighty-deploy-${data.aws_caller_identity.current.account_id}"
}

resource "aws_s3_bucket_public_access_block" "deploy" {
  bucket                  = aws_s3_bucket.deploy.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
```

Create `deploy/terraform/dns.tf` (zone only for now — the A record arrives in Task 4, the health check in Task 7; the domain is registered externally, so Terraform owns the zone and the registrar delegates to it):

```hcl
resource "aws_route53_zone" "main" {
  name = var.domain
}
```

Create `deploy/terraform/ssm.tf` (non-secret config the instance reads at deploy; secrets are created by CLI in Task 6, never here):

```hcl
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
```

Create `deploy/terraform/outputs.tf`:

```hcl
output "ecr_repo_url" {
  value = aws_ecr_repository.mighty.repository_url
}

output "deploy_bucket" {
  value = aws_s3_bucket.deploy.bucket
}

output "name_servers" {
  value = aws_route53_zone.main.name_servers
}
```

Create `deploy/terraform/terraform.tfvars.example`:

```hcl
domain      = "example.com"
alert_email = "you@example.com"
```

- [ ] **Step 2: Create the real tfvars and gitignore it**

```bash
cp deploy/terraform/terraform.tfvars.example deploy/terraform/terraform.tfvars
# edit deploy/terraform/terraform.tfvars with the real domain and email
```

Append to `.gitignore` (create if absent):

```gitignore
deploy/terraform/.terraform/
deploy/terraform/terraform.tfvars
*.tfstate
*.tfstate.*
```

(`.terraform.lock.hcl` IS committed — do not ignore it.)

- [ ] **Step 3: Init and apply**

```bash
tofu -chdir=deploy/terraform init
tofu -chdir=deploy/terraform apply
```

Expected: init prints "OpenTofu has been successfully initialized"; apply plan shows only additions (ECR repo + lifecycle policy, S3 bucket + public-access block, Route 53 zone, 2 SSM params), 0 to change/destroy. Type `yes`. Apply completes; outputs show `ecr_repo_url`, `deploy_bucket`, and `name_servers`.

- [ ] **Step 4: Delegate the domain to Route 53 at the registrar (manual)**

Run: `tofu -chdir=deploy/terraform output name_servers` — then in the registrar's dashboard (e.g. Porkbun → domain → Authoritative Nameservers), replace the default nameservers with the four AWS ones. Delegation must be live before Task 6 (Let's Encrypt can't validate until the domain resolves via Route 53).

- [ ] **Step 5: Verify delegation**

Run: `dig +short NS <domain>` (repeat until propagated — typically minutes, up to a few hours)
Expected: the four `awsdns` nameservers from the output above.

- [ ] **Step 6: Commit**

```bash
git add deploy/terraform .gitignore
git commit -m "infra: terraform scaffold with ECR, deploy bucket, config params"
```

---

### Task 4: Network, IAM, EC2 instance, EIP, DNS record

**Files:**
- Create: `deploy/terraform/network.tf`
- Create: `deploy/terraform/iam.tf`
- Create: `deploy/terraform/ec2.tf`
- Create: `deploy/terraform/user-data.sh`
- Modify: `deploy/terraform/dns.tf` (append the A record; zone resource exists from Task 3)
- Modify: `deploy/terraform/outputs.tf` (append)

**Interfaces:**
- Consumes: Task 3 variables/locals, `aws_ecr_repository.mighty`, `aws_s3_bucket.deploy`.
- Produces: running AL2023 ARM64 instance (Docker + compose + 2G swap + `/opt/mighty`), reachable only via SSM; `api.<domain>` A record → EIP; outputs `instance_id`, `public_ip` (Tasks 5–7 use `instance_id`, Task 7 attaches alarms). Volume tag `Snapshot = "true"` is the DLM target (Task 7).

- [ ] **Step 1: Write network.tf** (default VPC — a dedicated VPC buys nothing for one box):

```hcl
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

resource "aws_security_group" "api" {
  name        = "mighty-api"
  description = "Caddy ingress: HTTPS + ACME HTTP only. No SSH."
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTPS + WSS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTP for ACME challenges and HTTPS redirect"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

- [ ] **Step 2: Write iam.tf**

```hcl
resource "aws_iam_role" "ec2" {
  name = "mighty-api-ec2"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm_core" {
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy_attachment" "ecr_read" {
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
}

resource "aws_iam_role_policy" "app_access" {
  name = "mighty-app-access"
  role = aws_iam_role.ec2.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ReadAppParameters"
        Effect   = "Allow"
        Action   = ["ssm:GetParameter", "ssm:GetParameters"]
        Resource = "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/mighty/*"
      },
      {
        Sid      = "ReadDeployBundle"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:ListBucket"]
        Resource = [aws_s3_bucket.deploy.arn, "${aws_s3_bucket.deploy.arn}/*"]
      }
    ]
  })
}

resource "aws_iam_instance_profile" "ec2" {
  name = "mighty-api-ec2"
  role = aws_iam_role.ec2.name
}
```

- [ ] **Step 3: Write user-data.sh** (runs once at first boot):

```bash
#!/bin/bash
set -euxo pipefail

# Docker engine
dnf install -y docker
systemctl enable --now docker

# docker compose v2 plugin (not packaged in AL2023)
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL "https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-aarch64" \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# 2G swap — OOM insurance on a 2GB box (spec Section 1)
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab

# Unattended security updates (spec Section 3, Layer 3)
dnf install -y dnf-automatic
sed -i 's/^apply_updates = .*/apply_updates = yes/' /etc/dnf/automatic.conf
systemctl enable --now dnf-automatic.timer

# Deploy target directory
mkdir -p /opt/mighty
```

- [ ] **Step 4: Write ec2.tf**

```hcl
data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

resource "aws_instance" "api" {
  ami                    = data.aws_ssm_parameter.al2023_arm64.value
  instance_type          = var.instance_type
  subnet_id              = data.aws_subnets.default.ids[0]
  vpc_security_group_ids = [aws_security_group.api.id]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  user_data              = file("${path.module}/user-data.sh")

  metadata_options {
    http_tokens = "required" # IMDSv2 only
  }

  root_block_device {
    volume_size = 20
    volume_type = "gp3"
  }

  volume_tags = {
    Name     = "mighty-api"
    Snapshot = "true" # DLM nightly snapshot target (Task 7)
  }

  tags = { Name = "mighty-api" }

  lifecycle {
    ignore_changes = [ami] # AMI param moves daily; don't replace the box on apply
  }
}

resource "aws_eip" "api" {
  instance = aws_instance.api.id
  domain   = "vpc"
}
```

- [ ] **Step 5: Append the A record to dns.tf** (zone resource exists from Task 3)

```hcl
resource "aws_route53_record" "api" {
  zone_id = aws_route53_zone.main.zone_id
  name    = local.api_domain
  type    = "A"
  ttl     = 300
  records = [aws_eip.api.public_ip]
}
```

- [ ] **Step 6: Append to outputs.tf**

```hcl
output "instance_id" {
  value = aws_instance.api.id
}

output "public_ip" {
  value = aws_eip.api.public_ip
}
```

- [ ] **Step 7: Apply**

Run: `tofu -chdir=deploy/terraform apply`
Expected: additions only (SG, role + 2 attachments + inline policy, instance profile, instance, EIP, A record); 0 destroyed. Type `yes`.

- [ ] **Step 8: Verify SSM access and cloud-init results (no SSH — this proves the admin path)**

Wait ~3 minutes after apply, then:

```bash
INSTANCE_ID=$(tofu -chdir=deploy/terraform output -raw instance_id)
aws ssm start-session --target "$INSTANCE_ID"
```

Inside the session:

```bash
docker --version && docker compose version && swapon --show && ls -d /opt/mighty
exit
```

Expected: Docker ≥ 25, Compose v2.29.x, one 2G swap entry, `/opt/mighty` exists. (If `start-session` complains about a missing plugin: `brew install --cask session-manager-plugin`.)

- [ ] **Step 9: Verify DNS**

Run: `dig +short api.<domain>`
Expected: the EIP from `tofu -chdir=deploy/terraform output -raw public_ip`.

- [ ] **Step 10: Commit**

```bash
git add deploy/terraform
git commit -m "infra: EC2 instance with SSM-only access, EIP, and api DNS record"
```

---

### Task 5: Build and push the ARM64 image to ECR

**Files:** none created — uses Task 2's Dockerfile and Task 3's ECR repo.

**Interfaces:**
- Consumes: `ecr_repo_url` output; ARM64-capable Dockerfile.
- Produces: image `<account-id>.dkr.ecr.us-east-1.amazonaws.com/mighty:latest` (linux/arm64). Task 6's compose file pulls exactly this tag.

- [ ] **Step 1: Log in to ECR**

```bash
ECR_URL=$(tofu -chdir=deploy/terraform output -raw ecr_repo_url)
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin "${ECR_URL%%/*}"
```

Expected: `Login Succeeded`.

- [ ] **Step 2: Build and push**

```bash
docker buildx build --platform linux/arm64 -t "${ECR_URL}:latest" --push .
```

Expected: build completes and pushes; final line shows the digest.

- [ ] **Step 3: Verify the image landed with the right architecture**

```bash
aws ecr describe-images --repository-name mighty --region us-east-1 \
  --query 'imageDetails[].imageTags' --output text
docker buildx imagetools inspect "${ECR_URL}:latest"
```

Expected: `latest` tag present; imagetools output includes `Platform: linux/arm64`.

(No commit — nothing in the repo changed.)

---

### Task 6: Production compose bundle, secrets, and first deploy

**Files:**
- Create: `deploy/compose/docker-compose.prod.yml`
- Create: `deploy/compose/Caddyfile`
- Create: `deploy/compose/remote-deploy.sh`
- Create: `deploy/scripts/deploy.sh`

**Interfaces:**
- Consumes: `instance_id`/`deploy_bucket`/`ecr_repo_url` outputs; `:latest` image (Task 5); `/mighty/api_domain` + `/mighty/acme_email` params (Task 3); `/healthz` (Task 1); repo `migrations/` directory.
- Produces: the full stack live at `https://api.<domain>`; secrets at `/mighty/jwt_secret` + `/mighty/postgres_password`; `./deploy/scripts/deploy.sh` as the repeatable deploy command (Plan 5's CI reuses `remote-deploy.sh` verbatim).

- [ ] **Step 1: Create the app secrets in SSM (one-time, CLI — keeps them out of TF state)**

```bash
aws ssm put-parameter --region us-east-1 --name /mighty/jwt_secret \
  --type SecureString --value "$(openssl rand -base64 48)"
aws ssm put-parameter --region us-east-1 --name /mighty/postgres_password \
  --type SecureString --value "$(openssl rand -hex 24)"
```

Expected: each prints `"Version": 1`. (Postgres password is hex so it's URL-safe inside `POSTGRES_CONN`.)

- [ ] **Step 2: Write `deploy/compose/docker-compose.prod.yml`**

```yaml
# Production stack. Differences from dev docker-compose.yml are deliberate:
# only Caddy publishes host ports; schema comes from golang-migrate, not
# initdb.d; images are pinned/pulled, never built on the box.
services:
  caddy:
    image: caddy:2-alpine
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    environment:
      API_DOMAIN: ${API_DOMAIN}
      ACME_EMAIL: ${ACME_EMAIL}
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    depends_on:
      - mighty

  mighty:
    image: ${ECR_IMAGE}
    restart: unless-stopped
    env_file: .env
    depends_on:
      migrate:
        condition: service_completed_successfully
      redis:
        condition: service_started

  migrate:
    image: migrate/migrate:v4
    volumes:
      - ./migrations:/migrations:ro
    command: ["-path=/migrations", "-database", "${POSTGRES_CONN}", "up"]
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:16
    restart: unless-stopped
    environment:
      POSTGRES_USER: postgres
      POSTGRES_DB: postgres
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 3s
      retries: 12
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    volumes:
      - redis_data:/data

volumes:
  caddy_data:
  caddy_config:
  postgres_data:
  redis_data:
```

- [ ] **Step 3: Write `deploy/compose/Caddyfile`**

```caddyfile
{
	email {$ACME_EMAIL}
}

{$API_DOMAIN} {
	encode gzip
	reverse_proxy mighty:8080
}
```

(Caddy handles Let's Encrypt, HTTP→HTTPS redirect, and WebSocket upgrades automatically. Rate-limit zones, security headers, and CORS lockdown arrive in Plan 4.)

- [ ] **Step 4: Write `deploy/compose/remote-deploy.sh`** (runs ON the instance, as root, via SSM):

```bash
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
POSTGRES_PASSWORD=${PGPW}
POSTGRES_CONN=postgres://postgres:${PGPW}@postgres:5432/postgres?sslmode=disable
JWT_SECRET=$(param /mighty/jwt_secret)
REDIS_ADDR=redis:6379
LOG_LEVEL=info
API_DOMAIN=$(param /mighty/api_domain)
ACME_EMAIL=$(param /mighty/acme_email)
EOF

aws ecr get-login-password | docker login --username AWS --password-stdin "$ECR_HOST"
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d --remove-orphans
docker image prune -f
```

- [ ] **Step 5: Write `deploy/scripts/deploy.sh`** (runs on the workstation; also the Plan 5 CI entrypoint):

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TF=deploy/terraform
BUCKET=$(tofu -chdir="$TF" output -raw deploy_bucket)
INSTANCE_ID=$(tofu -chdir="$TF" output -raw instance_id)

aws s3 sync deploy/compose "s3://${BUCKET}/bundle/compose" --delete
aws s3 sync migrations "s3://${BUCKET}/bundle/migrations" --delete

# Parameters as strict JSON — shorthand parsing chokes on embedded quotes/newlines
PARAMS=$(printf '{"commands":["%s","%s","%s"]}' \
	"aws s3 sync s3://${BUCKET}/bundle/compose /opt/mighty --delete --region us-east-1" \
	"aws s3 sync s3://${BUCKET}/bundle/migrations /opt/mighty/migrations --delete --region us-east-1" \
	"bash /opt/mighty/remote-deploy.sh")

CMD_ID=$(aws ssm send-command \
	--region us-east-1 \
	--instance-ids "$INSTANCE_ID" \
	--document-name AWS-RunShellScript \
	--comment "mighty deploy" \
	--parameters "$PARAMS" \
	--query Command.CommandId --output text)

echo "Command: $CMD_ID — waiting..."
aws ssm wait command-executed --region us-east-1 \
	--command-id "$CMD_ID" --instance-id "$INSTANCE_ID" || true
aws ssm get-command-invocation --region us-east-1 \
	--command-id "$CMD_ID" --instance-id "$INSTANCE_ID" \
	--query '{Status:Status,Stdout:StandardOutputContent,Stderr:StandardErrorContent}' \
	--output json
```

Then: `chmod +x deploy/scripts/deploy.sh deploy/compose/remote-deploy.sh`

- [ ] **Step 6: Deploy**

Run: `./deploy/scripts/deploy.sh`
Expected: JSON output with `"Status": "Success"`; stdout shows compose pulling images and creating `caddy`, `mighty`, `migrate`, `postgres`, `redis`; migrate logs end with the schema version applied and no `error:` lines.

- [ ] **Step 7: Smoke-test HTTPS + health**

Run: `curl -i https://api.<domain>/healthz`
Expected: `HTTP/2 200`, body `ok`, valid certificate (no `-k` needed). If TLS fails, give Caddy ~60s for the first ACME issuance and retry; check `docker compose -f docker-compose.prod.yml logs caddy` via an SSM session.

- [ ] **Step 8: End-to-end game flow against production (REST + auth + DB + Redis)**

Run: `BASE_URL=https://api.<domain> ./scripts/demo_game_flow.sh`
Expected: registers 5 users, creates a game, 4 joins trigger the deal, first bid accepted — same output as against localhost. This proves Postgres writes, Redis hot state, and JWT auth through Caddy.

- [ ] **Step 9: Verify the WebSocket upgrade path through Caddy**

```bash
npx -y wscat -c "wss://api.<domain>/games/00000000-0000-0000-0000-000000000000/ws"
```

Expected: connection **opens** (TLS + upgrade succeeded through the proxy), then the server closes it (unknown game / 5-second unauthenticated AUTH deadline). Any response other than an HTTP-level proxy error proves WSS routing. For a full test, reuse a game ID + token printed by Step 8 and send the `{"type":"AUTH","token":"..."}` message the demo script prints.

- [ ] **Step 10: Verify reboot resilience**

```bash
aws ec2 reboot-instances --region us-east-1 --instance-ids \
  "$(tofu -chdir=deploy/terraform output -raw instance_id)"
sleep 120 && curl -is https://api.<domain>/healthz | head -1
```

Expected: `HTTP/2 200` — engine + `restart: unless-stopped` bring the stack back unattended.

- [ ] **Step 11: Commit**

```bash
git add deploy/compose deploy/scripts
git commit -m "infra: production compose stack with Caddy TLS and SSM-driven deploy"
```

---

### Task 7: Snapshots, dead-box alarms, health check, budget

**Files:**
- Create: `deploy/terraform/dlm.tf`
- Create: `deploy/terraform/alarms.tf`

**Interfaces:**
- Consumes: `aws_instance.api` (+ `Snapshot = "true"` volume tag), `aws_route53_health_check` → `/healthz` (Task 1, live since Task 6), `var.alert_email`.
- Produces: nightly EBS snapshots (7-day retention); SNS topic `mighty-alerts` with three alert paths (instance dead, API down, cost anomaly) per spec Section 4.

- [ ] **Step 1: Write dlm.tf**

```hcl
resource "aws_iam_role" "dlm" {
  name = "mighty-dlm"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "dlm.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "dlm" {
  role       = aws_iam_role.dlm.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSDataLifecycleManagerServiceRole"
}

resource "aws_dlm_lifecycle_policy" "nightly" {
  description        = "Nightly snapshots of mighty data volume, 7-day retention"
  execution_role_arn = aws_iam_role.dlm.arn
  state              = "ENABLED"

  policy_details {
    resource_types = ["VOLUME"]
    target_tags    = { Snapshot = "true" }

    schedule {
      name      = "nightly"
      copy_tags = true

      create_rule {
        interval      = 24
        interval_unit = "HOURS"
        times         = ["09:00"] # UTC ≈ 1–2am Pacific, low-traffic window
      }

      retain_rule {
        count = 7
      }
    }
  }
}
```

- [ ] **Step 2: Write alarms.tf**

```hcl
resource "aws_sns_topic" "alerts" {
  name = "mighty-alerts"
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# Dead-box alarm #1: the instance itself failed (spec Section 4)
resource "aws_cloudwatch_metric_alarm" "instance_dead" {
  alarm_name          = "mighty-instance-status-check-failed"
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed"
  dimensions          = { InstanceId = aws_instance.api.id }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
}

# Dead-box alarm #2: external probe — API unreachable from the internet
resource "aws_route53_health_check" "api" {
  fqdn              = local.api_domain
  port              = 443
  type              = "HTTPS"
  resource_path     = "/healthz"
  request_interval  = 30
  failure_threshold = 3
}

# Route 53 health check metrics are only published in us-east-1 — which is
# var.region here; revisit if the stack ever moves regions.
resource "aws_cloudwatch_metric_alarm" "api_down" {
  alarm_name          = "mighty-api-health-check-failed"
  namespace           = "AWS/Route53"
  metric_name         = "HealthCheckStatus"
  dimensions          = { HealthCheckId = aws_route53_health_check.api.id }
  statistic           = "Minimum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]
}

# Cost tripwire: $25 forecast / $30 actual (spec Section 4)
resource "aws_budgets_budget" "monthly" {
  name         = "mighty-monthly"
  budget_type  = "COST"
  limit_amount = "30"
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    notification_type          = "FORECASTED"
    comparison_operator        = "GREATER_THAN"
    threshold                  = 25
    threshold_type             = "ABSOLUTE_VALUE"
    subscriber_email_addresses = [var.alert_email]
  }

  notification {
    notification_type          = "ACTUAL"
    comparison_operator        = "GREATER_THAN"
    threshold                  = 30
    threshold_type             = "ABSOLUTE_VALUE"
    subscriber_email_addresses = [var.alert_email]
  }
}
```

- [ ] **Step 3: Apply and confirm the SNS subscription**

Run: `tofu -chdir=deploy/terraform apply`
Expected: additions only. Then check `<alert-email>`'s inbox for "AWS Notification - Subscription Confirmation" and click confirm (alarms silently no-op to email until confirmed).

- [ ] **Step 4: Verify the health check is passing**

```bash
HC_ID=$(aws route53 list-health-checks \
  --query 'HealthChecks[?HealthCheckConfig.FullyQualifiedDomainName==`api.<domain>`].Id' --output text)
aws route53 get-health-check-status --health-check-id "$HC_ID" \
  --query 'HealthCheckObservations[].StatusReport.Status' --output text | head -3
```

Expected: lines containing `Success: HTTP Status Code 200, OK`.

- [ ] **Step 5: Fire a real alarm end-to-end (stop Caddy, expect an email)**

Via SSM session on the instance: `docker stop $(docker ps -qf name=caddy)`, wait ~3 minutes.
Expected: `mighty-api-health-check-failed` goes ALARM and the email arrives. Then restart: `docker start $(docker ps -aqf name=caddy)` and confirm the OK email follows.

- [ ] **Step 6: Verify the DLM policy is active**

```bash
aws dlm get-lifecycle-policies --region us-east-1 \
  --query 'Policies[].{Id:PolicyId,State:State,Desc:Description}' --output table
```

Expected: the nightly policy with `State: ENABLED`. (First snapshot appears after the next 09:00 UTC window — check `aws ec2 describe-snapshots --owner-ids self` the following day.)

- [ ] **Step 7: Commit**

```bash
git add deploy/terraform/dlm.tf deploy/terraform/alarms.tf
git commit -m "infra: nightly EBS snapshots, dead-box alarms, and cost budget"
```

---

## Done criteria (whole plan)

- `https://api.<domain>/healthz` returns 200 with a valid certificate.
- `BASE_URL=https://api.<domain> ./scripts/demo_game_flow.sh` completes a full game setup.
- WSS upgrade succeeds through Caddy.
- Instance reboot self-heals; no SSH port exists; Postgres/Redis unreachable from the internet.
- Stopping Caddy produces an alert email within ~3 minutes.
- Budget + snapshot policies visible in the console.

## Explicitly deferred to later plans

- Cognito pool + JWKS auth swap (Plan 2), frontend auth + Amplify Hosting (Plan 3), xcaddy rate-limit build + Go rate middleware + security headers/CORS (Plan 4), OTel/Grafana Cloud + GitHub Actions CI/CD (Plan 5).
