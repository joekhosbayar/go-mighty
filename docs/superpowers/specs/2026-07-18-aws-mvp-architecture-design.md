# AWS MVP Architecture — Mighty (go-mighty + mighty-frontend)

**Date:** 2026-07-18
**Status:** All sections approved — awaiting final user review
**Budget:** Near-zero (~$19/mo + ~$14/yr domain; ~$12/mo fallback documented in Section 4)

## Goals

- Deploy the existing full stack (Go monolith with REST + WebSockets, Redis hot state/locking/pub-sub, Postgres) to AWS at near-zero cost.
- Upgrade authentication to professional grade: 2FA, passkeys (WebAuthn), password reset.
- Add proper safeguards: rate limiting, observability, alerting.
- No re-architecture of the stateful monolith (the event layer and engine stay as-is).

## Decisions Made

| Decision | Choice | Why |
|---|---|---|
| Budget posture | Near-zero (~$5–15/mo target) | User choice; buys one honest always-on box |
| Region | us-east-1 (N. Virginia) | User choice. Full service availability, cheapest AWS region. Trade-off: ~60–70ms RTT from LA vs ~20–30ms for us-west-2 — acceptable for a turn-based card game |
| Auth | Cognito User Pool (Essentials tier) | Passkeys/MFA/reset managed; free ≤10k MAU; backend swaps JWT issuing for JWKS verification |
| Domain | Buy via Route 53 (~$14/yr) | Passkeys (WebAuthn) require HTTPS on a real domain |
| Frontend hosting | Amplify Hosting | Free-tier CI/CD + CDN + TLS for the Vite app, plus built-in SPA URL-rewrite rule (serves `index.html` for client-routed paths so deep links/refreshes don't 404); no app code changes |
| Backend compute | Single EC2 t4g.small | Stateful websocket monolith needs an always-on box; ECS adds ALB cost (~$16/mo) with no MVP benefit. (Started as t4g.micro; bumped to small when the OTel Collector was added in Section 4) |
| Data tier | Postgres + Redis as containers on the same EC2 | User choice (revised from Aurora/ElastiCache exploration); cheapest, zero code changes; durability via EBS snapshots |
| Rate limiting | Caddy (edge, per-IP) + Go middleware (per-user/action) | Free and more precise than API Gateway throttling; API GW can't proxy the websocket anyway |
| Ingress | Caddy container (auto-TLS via Let's Encrypt) | On-box TLS is required regardless (WSS + passkeys); no ALB/NAT/API GW costs |

### Rejected alternatives

- **ECS Fargate + RDS + ElastiCache**: ~$40–50/mo minimum (ALB alone ~$16/mo). Post-MVP path.
- **API Gateway WebSockets + Lambda + DynamoDB**: fits free tier but forces a rewrite of the event layer and engine into stateless handlers.
- **API Gateway in front of EC2**: WebSocket API terminates sockets into request/response invocations (doesn't fit the pub/sub push model); HTTP API doesn't proxy WS upgrades; VPC Link needs an NLB/ALB; throttling is per-route, not per-client.
- **Aurora Serverless v2 + ElastiCache Serverless** (~$24–27/mo): explored and initially chosen, then reverted in favor of all-on-box (~$11/mo). Remains the graduation path when durability/scale demands it.
- **Lightsail**: cheaper flat pricing but no IAM instance roles, weak CloudWatch story, walled garden.
- **AWS WAF**: ~$6+/mo minimum; overkill for one box at MVP scale.

## Section 1 — Topology & Data Flow (APPROVED)

```
                      ┌──────────────────────────────────────────────┐
                      │                 AWS Account                   │
 ┌──────────┐  HTTPS  │  ┌────────────────┐     ┌──────────────────┐ │
 │ Browser/ │─────────┼─▶│ Amplify Hosting │     │ Cognito User Pool│ │
 │ Electron │◀────────┼──│ app.<domain>    │     │ (Essentials)     │ │
 └────┬─────┘         │  │ (Vite SPA+CDN)  │     └────────┬─────────┘ │
      │ HTTPS + WSS   │  └────────────────┘          JWKS │ (verify)  │
      │ api.<domain>  │  ┌────── VPC / public subnet ─────┼─────────┐ │
      │               │  │ ┌─────────────────────────────────────┐  │ │
      └───────────────┼─▶│ │ EC2 t4g.small — docker compose      │  │ │
                      │  │ │ ┌──────┐ ┌──────────┐               │  │ │
                      │  │ │ │Caddy │▶│ go-mighty│               │  │ │
                      │  │ │ │TLS+RL│ │ API+WS+  │               │  │ │
                      │  │ │ └──────┘ │ engine   │               │  │ │
                      │  │ │          └──┬────┬──┘               │  │ │
                      │  │ │   ┌─────────▼┐ ┌─▼────────┐         │  │ │
                      │  │ │   │ postgres │ │ redis    │         │  │ │
                      │  │ │   │ :16      │ │ 7-alpine │         │  │ │
                      │  │ │   └──────────┘ └──────────┘         │  │ │
                      │  │ │   (EBS gp3 volume, nightly DLM snap)│  │ │
                      │  │ └─────────────────────────────────────┘  │ │
                      │  └──────────────────────────────────────────┘ │
                      └──────────────────────────────────────────────┘
```

### Components

- **Amplify Hosting** (`app.<domain>`): builds `mighty-frontend` on push to main; CDN, managed TLS, SPA URL-rewrite rule (all client-routed paths serve `index.html`).
- **EC2 t4g.small** (`api.<domain>`): docker compose runs Caddy + go-mighty + postgres:16 + redis:7-alpine + OTel Collector (Section 4). Only Caddy publishes host ports (443, 80 for ACME). Postgres/Redis reachable only on the compose network. Security group inbound: 443 + 80 only. **No SSH port** — admin access via SSM Session Manager. Elastic IP attached; Route 53 A record.
- **Storage**: named volumes on a 20GB gp3 EBS volume. **Data Lifecycle Manager** nightly EBS snapshots, 7-day retention (DLM free; snapshot storage pennies). Optional later: nightly `pg_dump` to S3 as a second restore layer.
- **Sizing**: t4g.small (2GB) comfortably fits Go + Caddy + Postgres + Redis + OTel Collector; add a 2GB swapfile as OOM insurance. Budget fallback: t4g.micro (1GB) without the collector (~$12/mo total).
- **No NAT gateway, no ALB** — the two classic budget killers; neither is needed.

### Data flow (a move)

Client sends move over WSS → Caddy → go-mighty verifies the Cognito JWT captured at the 5-second AUTH handshake → engine validates against hot state in Redis (under distributed lock) → persists move to Postgres → publishes state diff via Redis pub/sub → all sockets in that game receive the update. Identical to today; only deployment changes.

### Cost breakdown

See the single authoritative cost summary at the end of Section 4 (~$19/mo total).

## Section 2 — Authentication (Cognito) (APPROVED)

### User pool (Essentials tier, free ≤10k MAU)

- Sign-in identifier: **email**; `preferred_username` required attribute for display. The `users` table stays authoritative for game-facing profile data; Cognito owns credentials.
- **`USER_AUTH` choice-based flow** with: password (SRP), **passkey (WebAuthn)**, email one-time code.
- **MFA:** optional TOTP (authenticator app), user-enrollable; enforced by Cognito once enrolled.
- **Password reset:** Cognito forgot-password flow (emailed code). Zero backend code.
- **Email delivery:** Cognito built-in sender (50/day cap) for MVP; upgrade path is SES.
- Brute-force lockout and auth throttling are built into Cognito.

### Frontend (mighty-frontend)

- `aws-amplify` v6, **embedded in the app's own UI** (no hosted-UI redirect). Provides signUp/confirmSignUp, signIn (USER_AUTH), passkey registration (`associateWebAuthnCredential`), TOTP setup, reset flows, automatic token storage + silent refresh.
- **Electron caveat:** WebAuthn works in recent Electron (Chromium; Touch ID on macOS) but must be tested early; fallback for Electron users is password+TOTP.

### Backend changes (go-mighty)

- Cognito issues RS256 **access tokens (1h)** + refresh tokens (30d). Access token goes in `Authorization: Bearer` (REST) and the websocket `AUTH` message (unchanged 5-second handshake).
- **Delete:** password hashing, `/register`, `/login`, JWT signing, `JWT_SECRET`.
- **Add:** JWKS verifier — fetch `https://cognito-idp.<region>.amazonaws.com/<pool-id>/.well-known/jwks.json` (cached, auto-refreshed; e.g. `lestrrat-go/jwx`); validate signature, `iss`, `client_id`, `token_use=access`, expiry. Swap inside auth middleware; handlers untouched.
- **User mapping:** Cognito `sub` (stable UUID) stored as `cognito_sub` on `users`; upsert on first authenticated request, seeding `username` from the token claim. Pre-launch clean cutover: drop the password column (no migration Lambda needed).
- **WebSocket token policy:** validate at connect only; socket persists past token expiry (a game shouldn't drop mid-hand). Reconnects re-auth with a fresh token.

## Section 3 — Safeguards: Rate Limiting & Hardening (APPROVED)

Defense in layers:

- **Layer 0 — Cognito (auth abuse):** login/signup/reset/OTP throttling and lockout are managed by Cognito; credential-stuffing never reaches our infrastructure.
- **Layer 1 — Caddy edge (per-IP, HTTP):** Caddy built with the rate-limit plugin via `xcaddy` (small custom Dockerfile).
  - General API zone ~100 req/min/IP → 429; strict zone ~10 req/min/IP on expensive endpoints (game creation, matchmaking).
  - Hygiene: body size cap (64KB), header/idle timeouts, HTTP→HTTPS redirect, security headers (HSTS, nosniff, frame-deny), CORS locked to `https://app.<domain>`.
- **Layer 2 — Go middleware (per-user/per-action):** token buckets in Redis keyed on Cognito `sub`.
  - Game creation ~10/hour/user.
  - WebSocket message cap ~10 msgs/sec (burst 20) per connection; violation drops the socket with a close code.
  - Concurrent sockets: max ~3/user, ~20/IP.
  - Keep the 5-second AUTH deadline; add max WS frame size and ping/pong idle timeout if absent.
- **Layer 3 — instance hardening:** SG inbound 443/80 only; no SSH (SSM Session Manager via IAM instance role); Postgres/Redis on compose network only (no host ports); secrets from SSM Parameter Store into `.env` at deploy (nothing in repo/user-data); `dnf-automatic` security updates (AL2023).

**Deferred with adoption triggers:** WAF (when ALB exists / real L7 attacks), CloudFront on the API (DDoS absorption at scale), Shield Advanced (never at this scale; Shield Standard is automatic/free).

## Section 4 — Observability (OpenTelemetry + Grafana Cloud), Alerting & CI/CD (APPROVED)

### Telemetry pipeline

One **OTel Collector (contrib)** container in the compose stack is the single telemetry pipeline; it observes sibling containers over the compose network. Backend: **Grafana Cloud free tier** (10k metric series, 50GB logs, 50GB traces/mo, 14-day retention — $0 at MVP volume; EC2 egress negligible within the 100GB/mo free allowance).

| Source | Mechanism | Signals |
|---|---|---|
| go-mighty | OTel Go SDK: `otelhttp`, `otelpgx`, `redisotel`, custom game spans/metrics | End-to-end move traces (WS frame → lock → engine → SQL → pub/sub), runtime metrics (goroutines, GC, heap) |
| Redis | Collector `redis` receiver (INFO scrape) | ops/sec, hit/miss, memory, clients, evictions |
| Postgres | Collector `postgresql` receiver | commits/rollbacks, rows, connections, table/index sizes |
| Per-container | Collector `docker_stats` receiver | CPU/mem/net/IO per container |
| Host | Collector `hostmetrics` receiver | replaces the CloudWatch agent |
| Logs | Containers → collector (or `awslogs` fallback) → Grafana Cloud Loki | JSON `slog` app logs, service logs |

- Collector runs with `memory_limiter` (~80–150MB RSS). **Instance sized t4g.small (2GB)** to accommodate the full stack comfortably (~+$6/mo vs micro).
- Trace sampling: head sampling ~10% baseline, 100% for errors (tail-based later if needed).
- Rejected backends: all-AWS EMF/X-Ray (CloudWatch $0.30/custom-metric trap — $20–50/mo unfiltered; viable only with strict allowlist), self-hosted Prom/Grafana/Tempo on-box (RAM cost; telemetry dies with the instance).

### Alerting

Dashboards and app-level alerts (error rate, latency, socket/game anomalies) live in **Grafana Cloud alerting** → email.

**Dead-box detection stays outside the OTel pipeline** (a box that dies can't export the telemetry saying so). Two independent CloudWatch alarms → SNS → email:

| Alarm | Condition |
|---|---|
| Instance dead | EC2 `StatusCheckFailed` |
| API down (external probe) | Route 53 health check on `https://api.<domain>` fails (~$0.75/mo — the only paid observability item) |

### CI/CD

- **Frontend:** Amplify builds/deploys on push to main (built in).
- **Backend:** GitHub Actions → build ARM64 image → push to ECR → deploy via **SSM Run Command** (`docker compose pull && up -d`). GitHub↔AWS auth via **OIDC federation** — no long-lived keys, no SSH from CI. Migrations run as a one-shot compose step (`golang-migrate` over existing `migrations/`) before the app starts.
- **IaC:** light Terraform/OpenTofu for hard-to-reproduce pieces — Cognito pool, IAM roles/OIDC, security groups, Route 53, DLM policy, EC2 + cloud-init (installs Docker, pulls compose). Goal: environment rebuildable in ~20 minutes.

### Final cost summary

| Item | $/mo |
|---|---|
| EC2 t4g.small on-demand | ~12.20 |
| EBS 20GB gp3 + DLM snapshots | ~1.80 |
| Public IPv4 | ~3.65 |
| Route 53 hosted zone + health check | ~1.25 |
| Grafana Cloud, Cognito, Amplify, ECR, CloudWatch, SNS, SSM | ~0 (free tiers) |
| **Total** | **~$19/mo** + ~$14/yr domain |

(Fallback to t4g.micro + no collector ≈ $12/mo if budget pressure returns.)

## Known Frontend Prerequisite (out of scope for this spec, blocks launch)

**Mid-game refresh boots the player back to the open-tables page.** This is client-side state restoration, not hosting: on refresh the websocket and in-memory game store die, the app boots fresh and defaults to the lobby. Amplify's SPA URL-rewrite does **not** fix this — it only prevents 404s on deep links. Required fix in `mighty-frontend`:

1. Encode the active game in the URL (`/games/:id`) so refresh carries a resume key.
2. Rejoin-on-load: on mount with a game ID present, fetch game state from the backend (hot state already lives in Redis), reopen the websocket, re-AUTH, resubscribe — instead of redirecting to the lobby when the store is empty.

For a card game, an accidental refresh mid-hand currently abandons the game — treat as a launch blocker, tracked separately from this architecture work.
