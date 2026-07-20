# AWS Infra Foundation — Command & Provisioning Log

Plan: `go-mighty/docs/superpowers/plans/2026-07-19-aws-infra-foundation.md`
Account: `711387141487` · Region: `us-east-1` · Domain: `themighty.gg` (registered at Porkbun, DNS delegated to Route 53)

## Summary: what now exists in AWS

| Resource | Identifier | Purpose |
|---|---|---|
| S3 bucket (TF state) | `mighty-tfstate-711387141487` | Terraform remote state, versioned |
| S3 bucket (deploy bundle) | `mighty-deploy-711387141487` | Compose files + migrations synced here, pulled by the instance |
| ECR repository | `mighty` | ARM64 app image, tag `latest`, lifecycle keeps last 10 |
| Route 53 hosted zone | `themighty.gg` | DNS, delegated from Porkbun to AWS nameservers |
| Route 53 A record | `api.themighty.gg` → `50.17.190.7` | Points at the EC2 Elastic IP |
| Route 53 health check | `acf21929-6cb1-476b-813d-32710fe89003` | HTTPS probe of `/healthz` every 30s |
| EC2 instance | `i-0f9d659dbcc633812` (t4g.small, us-east-1d) | Runs the whole docker-compose stack |
| Elastic IP | `50.17.190.7` | Static IP attached to the instance |
| Security group | `mighty-api` (`sg-0e373a6762e332d6c`) | Inbound 443 + 80 only, no SSH |
| IAM role + instance profile | `mighty-api-ec2` | SSM Session Manager + ECR pull + scoped SSM param/S3 read |
| SSM parameters | `/mighty/api_domain`, `/mighty/acme_email` (plain), `/mighty/jwt_secret`, `/mighty/postgres_password` (SecureString) | App config + secrets, rendered to `.env` on deploy |
| DLM lifecycle policy | `policy-06654b0916523c273` | Nightly EBS snapshot of the data volume (tag `Snapshot=true`), 09:00 UTC, 7-day retention |
| SNS topic | `mighty-alerts` | Fan-out for both CloudWatch alarms |
| SNS subscription | `joekhosbayar123@gmail.com` | ⚠️ currently shows `Deleted` — needs re-fix, see Outstanding Issues |
| CloudWatch alarm | `mighty-instance-status-check-failed` | EC2 `StatusCheckFailed` → SNS |
| CloudWatch alarm | `mighty-api-health-check-failed` | Route 53 health check → SNS |
| AWS Budget | `mighty-monthly` | $25 forecast / $30 actual → email (native, no confirmation needed) |
| Docker image | `711387141487.dkr.ecr.us-east-1.amazonaws.com/mighty:latest` | linux/arm64, pushed from local buildx |

**Live endpoint:** `https://api.themighty.gg` — Caddy (auto TLS) → go-mighty (Postgres + Redis), on the same box.

---

## Task 0 — Manual prerequisites (run by controller)

```bash
aws sts get-caller-identity --query Account --output text
rtk proxy aws sts get-caller-identity
rtk proxy aws route53 list-hosted-zones --query 'HostedZones[].Name' --output text

brew install opentofu   # -> OpenTofu v1.12.4

rtk proxy aws s3api create-bucket --bucket mighty-tfstate-711387141487 --region us-east-1
rtk proxy aws s3api put-bucket-versioning --bucket mighty-tfstate-711387141487 \
  --versioning-configuration Status=Enabled
rtk proxy aws s3api get-bucket-versioning --bucket mighty-tfstate-711387141487
```
**Provisions:** the Terraform state bucket only. Domain purchased manually by the user at Porkbun (external action, not a CLI command).

---

## Task 1 — `/healthz` endpoint (code only, no AWS)

TDD: wrote `internal/api/health.go` + `internal/api/health_test.go`, wired `GET /healthz` into `cmd/server/main.go`.
```bash
go test ./internal/api/ -run TestHealthzHandler -v
go test ./...
```
**Commit:** `690df85`. Nothing uploaded or provisioned — pure code change.

---

## Task 2 — ARM64 Docker build (local verification only, no AWS)

Edited `Dockerfile` (`--platform=$BUILDPLATFORM`, `ARG TARGETARCH`, `GOARCH=${TARGETARCH}`) and `scripts/demo_game_flow.sh` (`BASE_URL` override).
```bash
docker buildx build --platform linux/arm64 -t mighty:arm64-check --load .
docker inspect mighty:arm64-check --format '{{.Architecture}}'   # -> arm64
docker compose build
```
**Commit:** `f56ee02`. Nothing uploaded to AWS — this only proved the Dockerfile cross-compiles.

---

## Task 3 — Terraform scaffold, ECR, deploy bucket, hosted zone

Wrote `deploy/terraform/{versions,variables,ecr,s3,dns,ssm,outputs}.tf`, `terraform.tfvars.example`, `.gitignore` additions.
```bash
tofu -chdir=deploy/terraform init
tofu -chdir=deploy/terraform apply   # 7 added, 0 changed, 0 destroyed
```
**Provisions:**
- `aws_ecr_repository.mighty` + `aws_ecr_lifecycle_policy.mighty`
- `aws_s3_bucket.deploy` (`mighty-deploy-711387141487`) + `aws_s3_bucket_public_access_block.deploy`
- `aws_route53_zone.main` (`themighty.gg`)
- `aws_ssm_parameter.api_domain`, `aws_ssm_parameter.acme_email`

**Manual step:** the four AWS nameservers from the `name_servers` output were pasted into Porkbun's Authoritative Nameservers by the user; verified with `dig +short NS themighty.gg`.

**Commit:** `98f1698`.

---

## Task 4 — Networking, IAM, EC2, EIP, DNS A record

Wrote `deploy/terraform/{network,iam,ec2}.tf`, `user-data.sh`; appended to `dns.tf` and `outputs.tf`.
```bash
tofu -chdir=deploy/terraform apply
```
**Provisions:**
- `aws_security_group.api` (`mighty-api`) — inbound 443/80 only
- `aws_iam_role.ec2` (`mighty-api-ec2`) + `AmazonSSMManagedInstanceCore` + `AmazonEC2ContainerRegistryReadOnly` + inline policy scoped to `parameter/mighty/*` and the deploy bucket
- `aws_iam_instance_profile.ec2`
- `aws_instance.api` — t4g.small, IMDSv2 required, 20GB gp3 root volume tagged `Snapshot=true`, cloud-init (`user-data.sh`) installs Docker + compose plugin + 2G swap + `dnf-automatic`
- `aws_eip.api` → `50.17.190.7`
- `aws_route53_record.api` — A record `api.themighty.gg` → the EIP

Verification (no SSH — SSM only):
```bash
aws ssm describe-instance-information --filters "Key=InstanceIds,Values=i-0f9d659dbcc633812"
aws ssm send-command ... "docker --version && docker compose version && swapon --show && ls -d /opt/mighty"
dig +short api.themighty.gg @ns-434.awsdns-54.com
```
**Commit:** `b95faa9`.

### Fix wave (review found: non-deterministic subnet selection could force-replace the instance on a future apply; dnf-automatic wasn't security-only)
- `ec2.tf`: `subnet_id = sort(data.aws_subnets.default.ids)[0]`; `lifecycle.ignore_changes = [ami, subnet_id]`
- `user-data.sh`: added `sed ... upgrade_type = security`
- Live instance patched via `aws ssm send-command` (sed + `systemctl restart dnf-automatic.timer`)
```bash
tofu -chdir=deploy/terraform validate
tofu -chdir=deploy/terraform plan    # in-place user_data update only, no replacement
tofu -chdir=deploy/terraform apply   # 0 added, 1 changed, 0 destroyed
tofu -chdir=deploy/terraform plan    # No changes
```
**Commit:** `55b60b8`.

---

## Task 5 — Build and push the ARM64 image (no repo changes)

```bash
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin 711387141487.dkr.ecr.us-east-1.amazonaws.com
docker buildx build --platform linux/arm64 -t 711387141487.dkr.ecr.us-east-1.amazonaws.com/mighty:latest --push .
aws ecr describe-images --repository-name mighty --region us-east-1
docker buildx imagetools inspect 711387141487.dkr.ecr.us-east-1.amazonaws.com/mighty:latest
```
**Uploads:** `mighty:latest` (linux/arm64) to ECR — digest `sha256:20f14d2eb0578b0a733ea885188bd7336adc9b7214158560935bf3bd945e1966`. No commit (nothing in the repo changed).

---

## Task 6 — Production compose stack, secrets, first deploy

```bash
aws ssm put-parameter --name /mighty/jwt_secret --type SecureString --value "$(openssl rand -base64 48)"
aws ssm put-parameter --name /mighty/postgres_password --type SecureString --value "$(openssl rand -hex 24)"
```
Wrote `deploy/compose/docker-compose.prod.yml`, `deploy/compose/Caddyfile`, `deploy/compose/remote-deploy.sh`, `deploy/scripts/deploy.sh`.
```bash
./deploy/scripts/deploy.sh
```
**What this uploads/does:**
- `aws s3 sync` pushes `deploy/compose/` and `migrations/` to `s3://mighty-deploy-711387141487/bundle/...`
- `aws ssm send-command` on the instance: syncs the bundle from S3 to `/opt/mighty`, runs `remote-deploy.sh` — which reads secrets from SSM, writes `.env` (mode 600), logs into ECR, `docker compose pull` + `up -d` (services: `caddy`, `mighty`, `migrate`, `postgres`, `redis`)

Smoke tests:
```bash
curl -i https://api.themighty.gg/healthz                       # HTTP/2 200, valid cert
BASE_URL=https://api.themighty.gg ./scripts/demo_game_flow.sh  # full 5-player game flow
curl (WS upgrade headers) https://api.themighty.gg/games/.../ws  # proves WSS routes through Caddy
aws ec2 reboot-instances --instance-ids i-0f9d659dbcc633812     # reboot resilience: healthz 200 within ~30s
```
**Commit:** `6b48441`.

### Fix wave (review found: `migrate/migrate:4` is a floating tag, not a pin)
- `docker-compose.prod.yml`: `migrate/migrate:4` → `migrate/migrate:v4.19.1`
- Redeployed via `./deploy/scripts/deploy.sh`; verified via SSM `docker compose ps -a` → `migrate/migrate:v4.19.1 ... Exited (0)`
**Commit:** `1e270ad`.

---

## Task 7 — Snapshots, dead-box alarms, health check, budget

Wrote `deploy/terraform/dlm.tf`, `deploy/terraform/alarms.tf`.
```bash
tofu -chdir=deploy/terraform apply   # 9 added, 0 changed, 0 destroyed
```
**Provisions:**
- `aws_iam_role.dlm` (`mighty-dlm`) + `AWSDataLifecycleManagerServiceRole` attachment
- `aws_dlm_lifecycle_policy.nightly` — targets tag `Snapshot=true`, 24h interval @ 09:00 UTC, retain 7
- `aws_sns_topic.alerts` (`mighty-alerts`) + `aws_sns_topic_subscription.email` (joekhosbayar123@gmail.com)
- `aws_cloudwatch_metric_alarm.instance_dead` — EC2 `StatusCheckFailed`
- `aws_route53_health_check.api` — HTTPS, port 443, path `/healthz`, 30s interval, failure_threshold 3
- `aws_cloudwatch_metric_alarm.api_down` — Route53 `HealthCheckStatus`, `treat_missing_data=breaching`
- `aws_budgets_budget.monthly` — $25 FORECASTED / $30 ACTUAL → email

**Deviation:** the DLM `description` field originally contained a comma, which AWS's DLM API rejects (`^[0-9A-Za-z _-]+$`); comma removed, plan doc patched to match.

**Commit:** `8402fed` (+ a docs-only commit fixing the same comma in the plan file).

### Post-apply: SNS subscription confirmation (in progress)
```bash
aws sns list-subscriptions-by-topic --topic-arn arn:aws:sns:us-east-1:711387141487:mighty-alerts
tofu -chdir=deploy/terraform apply -replace=aws_sns_topic_subscription.email   # re-run by the user after first confirmation attempt failed
```
Subscription still shows `SubscriptionArn: Deleted` as of the last check — **not yet resolved**, see below.

---

## Outstanding before Task 7 is closed out

1. **SNS email subscription still `Deleted`.** Two confirm attempts haven't stuck. The two dead-box alarms (`instance_dead`, `api_down`) have nowhere to deliver until this is fixed — they'd fire silently.
2. **Alarm-fire end-to-end test** (plan Task 7 Step 5: stop Caddy ~3 min, expect an alarm email, restart, expect the OK email) is blocked on #1.
3. **Final whole-branch review** across all 7 tasks — not yet run.
