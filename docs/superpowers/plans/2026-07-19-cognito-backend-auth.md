# Cognito Backend Auth Implementation Plan (Plan 2 of 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace go-mighty's homegrown password/HS256-JWT auth with Cognito: the backend verifies Cognito RS256 access tokens via JWKS and maps them to local users; all password code is deleted.

**Architecture:** Terraform adds a Cognito User Pool (Essentials, USER_AUTH flow with password/passkey/email-OTP, optional TOTP) and SPA client. A new `service.CognitoAuth` validates `Authorization: Bearer` and WebSocket-AUTH tokens against the pool's JWKS (cached via `lestrrat-go/jwx/v2`), then upserts a local `users` row keyed on `cognito_sub`. `/auth/signup`, `/auth/login`, bcrypt, and `JWT_SECRET` are deleted. Handlers and the game engine are untouched — they keep receiving `*service.AuthClaims{UserID, Username}`.

**Tech Stack:** OpenTofu (existing `deploy/terraform/` workspace), `github.com/lestrrat-go/jwx/v2`, `github.com/aws/aws-sdk-go-v2` (`config` + `cognitoidentityprovider`, for the one-time display-name lookup), golang-migrate migration `000002`, existing sqlmock/httptest test patterns.

**Spec:** `docs/superpowers/specs/2026-07-18-aws-mvp-architecture-design.md` Section 2. Out of scope: all frontend work (Plan 3 — the embedded aws-amplify UI is the user-facing signup path; this plan's CLI-created users are for verification only).

**Documented deviation from spec:** the spec says to seed `username` "from the token claim," but with `username_attributes = ["email"]` the access token's `username` claim is just the sub UUID — `preferred_username` only appears in ID tokens, which we deliberately don't accept. Instead, on first-seen sub the backend calls `AdminGetUser` (new IAM permission, instance role) to fetch `preferred_username`, falling back to the sub on any error. Auth never fails because of a display-name lookup.

## Global Constraints

- Region **us-east-1**; account 711387141487; domain `themighty.gg`; existing tofu workspace `deploy/terraform/` (S3 backend, applied through Plan 1).
- Issuer must be exactly `https://cognito-idp.us-east-1.amazonaws.com/<pool-id>`; tokens accepted ONLY if: RS256 signature verifies against the pool JWKS, `iss` matches, `client_id` claim == the SPA client id, `token_use == "access"`, not expired.
- Access token validity **60 min**, refresh **30 days**, ID token 60 min; SPA client has **no client secret**.
- WebSocket policy (spec): validate at connect only; the socket persists past token expiry — current `ws.go` behavior already does this; do not add mid-connection revalidation.
- Backend config via env: `COGNITO_POOL_ID`, `COGNITO_CLIENT_ID`, `COGNITO_REGION` (default `us-east-1`); rendered into prod `.env` from SSM params `/mighty/cognito_pool_id`, `/mighty/cognito_client_id`. `JWT_SECRET` is deleted everywhere.
- Pre-launch **clean cutover**: migration `000002` deletes existing (demo) users; no data migration.
- Every task must leave the repo compiling and `go test ./...` green. Production stack at `https://api.themighty.gg` must stay up; it only changes in Task 5.
- All commits on branch `feature/cognito-backend-auth`. rtk hook caveat: if CLI output looks garbled, re-run with `rtk proxy ` prefix.

---

### Task 1: Terraform — Cognito User Pool, SPA client, SSM params, IAM

**Files:**
- Create: `deploy/terraform/cognito.tf`
- Modify: `deploy/terraform/iam.tf` (append one statement to the `app_access` inline policy)
- Modify: `deploy/terraform/ssm.tf` (append two params)
- Modify: `deploy/terraform/outputs.tf` (append two outputs)

**Interfaces:**
- Consumes: existing `var.domain`, `aws_iam_role_policy.app_access`, Plan-1 workspace.
- Produces: `aws_cognito_user_pool.main`, `aws_cognito_user_pool_client.spa`; SSM `/mighty/cognito_pool_id` + `/mighty/cognito_client_id`; outputs `cognito_pool_id`, `cognito_client_id`. Tasks 3–5 consume the pool id/client id; the instance role gains `cognito-idp:AdminGetUser`.

- [ ] **Step 1: Refresh the provider** (sign_in_policy/web_authn/user_pool_tier need a recent AWS provider)

Run: `tofu -chdir=deploy/terraform init -upgrade`
Expected: provider `hashicorp/aws` updates within `~> 5.0`. If later steps error with "Unsupported block type" on `sign_in_policy` or `web_authn_configuration`, STOP and report BLOCKED (provider too old for pinned major) rather than removing the blocks.

- [ ] **Step 2: Write `deploy/terraform/cognito.tf`**

```hcl
resource "aws_cognito_user_pool" "main" {
  name                = "mighty-users"
  user_pool_tier      = "ESSENTIALS"
  deletion_protection = "ACTIVE"

  # Sign in with email; Cognito generates an opaque internal username (== sub).
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  # USER_AUTH choice-based first factors (spec Section 2).
  sign_in_policy {
    allowed_first_auth_factors = ["PASSWORD", "WEB_AUTHN", "EMAIL_OTP"]
  }

  # Passkeys: RP id is the apex so app.themighty.gg can register credentials.
  web_authn_configuration {
    relying_party_id  = var.domain
    user_verification = "preferred"
  }

  mfa_configuration = "OPTIONAL"
  software_token_mfa_configuration {
    enabled = true
  }

  schema {
    name                = "preferred_username"
    attribute_data_type = "String"
    required            = true
    mutable             = true

    string_attribute_constraints {
      min_length = 1
      max_length = 128
    }
  }

  password_policy {
    minimum_length    = 8
    require_lowercase = true
    require_uppercase = true
    require_numbers   = true
    require_symbols   = false
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }
}

resource "aws_cognito_user_pool_client" "spa" {
  name         = "mighty-spa"
  user_pool_id = aws_cognito_user_pool.main.id

  # ADMIN_USER_PASSWORD_AUTH exists solely for CLI-scripted test users
  # (demo/e2e); remove when Plan 3's real signup UI is live.
  explicit_auth_flows = [
    "ALLOW_USER_AUTH",
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_ADMIN_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]

  generate_secret = false

  access_token_validity  = 60
  id_token_validity      = 60
  refresh_token_validity = 30

  token_validity_units {
    access_token  = "minutes"
    id_token      = "minutes"
    refresh_token = "days"
  }

  prevent_user_existence_errors = "ENABLED"
}
```

- [ ] **Step 3: Append to `deploy/terraform/ssm.tf`**

```hcl
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
```

- [ ] **Step 4: Append to `deploy/terraform/outputs.tf`**

```hcl
output "cognito_pool_id" {
  value = aws_cognito_user_pool.main.id
}

output "cognito_client_id" {
  value = aws_cognito_user_pool_client.spa.id
}
```

- [ ] **Step 5: Extend the instance role in `deploy/terraform/iam.tf`** — add this statement to the `Statement` list of `aws_iam_role_policy.app_access` (display-name lookup on first login):

```hcl
      {
        Sid      = "CognitoDisplayNameLookup"
        Effect   = "Allow"
        Action   = ["cognito-idp:AdminGetUser"]
        Resource = aws_cognito_user_pool.main.arn
      }
```

- [ ] **Step 6: Apply**

Run: `tofu -chdir=deploy/terraform apply`
Expected: additions only (pool, client, 2 SSM params) plus ONE in-place update (`aws_iam_role_policy.app_access`); 0 destroyed. Anything else → BLOCKED with the plan output.

- [ ] **Step 7: Verify token issuance end-to-end via CLI**

```bash
POOL_ID=$(tofu -chdir=deploy/terraform output -raw cognito_pool_id)
CLIENT_ID=$(tofu -chdir=deploy/terraform output -raw cognito_client_id)

aws cognito-idp admin-create-user --region us-east-1 --user-pool-id "$POOL_ID" \
  --username smoke@example.com --message-action SUPPRESS \
  --user-attributes Name=email,Value=smoke@example.com Name=email_verified,Value=true Name=preferred_username,Value=smoketest

aws cognito-idp admin-set-user-password --region us-east-1 --user-pool-id "$POOL_ID" \
  --username smoke@example.com --password 'MightySmoke1' --permanent

ACCESS=$(aws cognito-idp admin-initiate-auth --region us-east-1 --user-pool-id "$POOL_ID" \
  --client-id "$CLIENT_ID" --auth-flow ADMIN_USER_PASSWORD_AUTH \
  --auth-parameters USERNAME=smoke@example.com,PASSWORD=MightySmoke1 \
  --query 'AuthenticationResult.AccessToken' --output text)

python3 -c 'import base64,json,sys; p=sys.argv[1].split(".")[1]; p+="="*(-len(p)%4); print(json.dumps(json.loads(base64.urlsafe_b64decode(p)),indent=1))' "$ACCESS"
```

Expected payload: `"iss": "https://cognito-idp.us-east-1.amazonaws.com/<POOL_ID>"`, `"token_use": "access"`, `"client_id": "<CLIENT_ID>"`, `"sub"` = a UUID, `"username"` = same UUID. Also verify the display name is retrievable (this is what the backend will do):

```bash
aws cognito-idp admin-get-user --region us-east-1 --user-pool-id "$POOL_ID" --username smoke@example.com \
  --query "UserAttributes[?Name=='preferred_username'].Value" --output text
```

Expected: `smoketest`. Then clean up:

```bash
aws cognito-idp admin-delete-user --region us-east-1 --user-pool-id "$POOL_ID" --username smoke@example.com
```

- [ ] **Step 8: Commit**

```bash
git add deploy/terraform/cognito.tf deploy/terraform/iam.tf deploy/terraform/ssm.tf deploy/terraform/outputs.tf
git commit -m "infra: Cognito user pool (USER_AUTH + passkeys + optional TOTP) and SPA client"
```

---

### Task 2: Migration 000002 — cognito_sub in, password_hash out

**Files:**
- Create: `migrations/000002_cognito_cutover.up.sql`
- Create: `migrations/000002_cognito_cutover.down.sql`

**Interfaces:**
- Consumes: schema from `000001` (users: `id`, `username` UNIQUE NOT NULL, `password_hash`, `email` UNIQUE nullable).
- Produces: `users.cognito_sub VARCHAR(64) UNIQUE NOT NULL`, no `password_hash`. Task 3's store code and Task 5's prod deploy depend on exactly this shape.

- [ ] **Step 1: Write `migrations/000002_cognito_cutover.up.sql`**

```sql
-- Pre-launch clean cutover (spec Section 2): existing rows are demo users
-- with no Cognito identity. user_stats cascades; moves.player_id has no FK.
DELETE FROM users;

ALTER TABLE users ADD COLUMN cognito_sub VARCHAR(64) UNIQUE NOT NULL;
ALTER TABLE users DROP COLUMN password_hash;
```

- [ ] **Step 2: Write `migrations/000002_cognito_cutover.down.sql`**

```sql
DELETE FROM users;

ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users DROP COLUMN cognito_sub;
```

- [ ] **Step 3: Verify up AND down against a throwaway Postgres**

```bash
docker run -d --name migtest -e POSTGRES_PASSWORD=migtest -p 55432:5432 postgres:16
sleep 5
DB='postgres://postgres:migtest@localhost:55432/postgres?sslmode=disable'
docker run --rm -v "$PWD/migrations:/migrations:ro" --network host migrate/migrate:v4.19.1 -path=/migrations -database "$DB" up
docker exec migtest psql -U postgres -c '\d users'
docker run --rm -v "$PWD/migrations:/migrations:ro" --network host migrate/migrate:v4.19.1 -path=/migrations -database "$DB" down 1
docker exec migtest psql -U postgres -c '\d users'
docker rm -f migtest
```

Expected: after `up`, `\d users` shows `cognito_sub` (with unique index) and no `password_hash`; after `down 1`, `password_hash` is back and `cognito_sub` gone. (On Docker Desktop for Mac `--network host` doesn't reach the published port — use `-database 'postgres://postgres:migtest@host.docker.internal:55432/...'` without `--network host` instead.)

- [ ] **Step 4: Commit**

```bash
git add migrations/000002_cognito_cutover.up.sql migrations/000002_cognito_cutover.down.sql
git commit -m "feat: migration 000002 - cognito_sub cutover, drop password_hash"
```

---

### Task 3: Store upsert + CognitoAuth service (TDD, additive — old auth untouched)

**Files:**
- Modify: `internal/store/postgres/user_store.go` (add `CognitoSub` field; add `GetUserByCognitoSub`, `UpsertUserByCognitoSub`)
- Create: `internal/store/postgres/user_store_test.go`
- Create: `internal/infra/cognito_client.go`
- Create: `internal/service/cognito_auth.go`
- Create: `internal/service/cognito_auth_test.go`

**Interfaces:**
- Consumes: `postgres.Store` (`s.db`), existing `AuthClaims` type (from `auth_service.go`, deleted in Task 4 — this task only SETS `UserID`/`Username` on it, never the embedded JWT claims).
- Produces (Task 4 wires these in):
  - `postgres.Store.UpsertUserByCognitoSub(ctx context.Context, sub, username string) (*User, error)`
  - `service.NewCognitoAuth(ctx context.Context, store *postgres.Store, fetcher UserAttributesFetcher, issuer, clientID string) (*CognitoAuth, error)`
  - `(*CognitoAuth).ValidateToken(ctx context.Context, token string) (*AuthClaims, error)`
  - `service.UserAttributesFetcher` interface `{ PreferredUsername(ctx context.Context, sub string) (string, error) }` and its prod impl `infra.NewCognitoAttributesFetcher(ctx context.Context, region, poolID string) (*infra.CognitoAttributesFetcher, error)`

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/lestrrat-go/jwx/v2@latest
go get github.com/aws/aws-sdk-go-v2/config@latest github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider@latest
```

- [ ] **Step 2: Extend the `User` struct in `internal/store/postgres/user_store.go`** — add `CognitoSub string` after `Username` (keep `PasswordHash` for now; it dies in Task 4).

- [ ] **Step 3: Write failing store tests** in `internal/store/postgres/user_store_test.go` (in-package sqlmock test — construct the store as `&Store{db: db}`; read `postgres_client.go` first and adjust if the db field is named differently):

```go
package postgres

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestUpsertUserByCognitoSub_CreatesOnFirstSight(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{db: db}

	// Fast-path lookup misses first (the upsert SELECTs before opening a tx).
	empty := sqlmock.NewRows([]string{"id", "username", "cognito_sub", "email", "created_at", "updated_at"})
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, cognito_sub`)).
		WithArgs("sub-123").WillReturnRows(empty)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users`)).
		WithArgs("sub-123", "alice", "sub-123").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO user_stats`)).
		WithArgs("sub-123").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	rows := sqlmock.NewRows([]string{"id", "username", "cognito_sub", "email", "created_at", "updated_at"}).
		AddRow("sub-123", "alice", "sub-123", nil, testTime(), testTime())
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, cognito_sub`)).
		WithArgs("sub-123").WillReturnRows(rows)

	user, err := s.UpsertUserByCognitoSub(context.Background(), "sub-123", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "sub-123" || user.Username != "alice" {
		t.Fatalf("got %+v", user)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetUserByCognitoSub_NotFoundReturnsNil(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, cognito_sub`)).
		WithArgs("nope").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "cognito_sub", "email", "created_at", "updated_at"}))

	user, err := s.GetUserByCognitoSub(context.Background(), "nope")
	if err != nil || user != nil {
		t.Fatalf("want nil,nil got %v,%v", user, err)
	}
}
```

(Add a tiny `func testTime() time.Time { return time.Unix(0, 0) }` helper. Adjust the `Store` literal if the struct's db field has a different name — read the file first.)

- [ ] **Step 4: Run tests — expect FAIL** (`UpsertUserByCognitoSub` undefined): `go test ./internal/store/postgres/ -run 'CognitoSub' -v`

- [ ] **Step 5: Implement in `user_store.go`**

```go
// GetUserByCognitoSub retrieves a user by their Cognito subject id.
func (s *Store) GetUserByCognitoSub(ctx context.Context, sub string) (*User, error) {
	query := `SELECT id, username, cognito_sub, email, created_at, updated_at FROM users WHERE cognito_sub = $1`

	var (
		user  User
		email sql.NullString
	)

	err := s.db.QueryRowContext(ctx, query, sub).Scan(&user.ID, &user.Username, &user.CognitoSub, &email, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	user.Email = email.String

	return &user, nil
}

// UpsertUserByCognitoSub returns the local user for a Cognito subject,
// creating one (with stats) on first sight. Safe under concurrent first
// requests: the insert is ON CONFLICT DO NOTHING keyed on cognito_sub.
// A username collision (preferred_username is not unique in Cognito)
// falls back to a sub-suffixed username.
func (s *Store) UpsertUserByCognitoSub(ctx context.Context, sub, username string) (*User, error) {
	if existing, err := s.GetUserByCognitoSub(ctx, sub); err != nil || existing != nil {
		return existing, err
	}

	insert := func(name string) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, username, cognito_sub, email, created_at, updated_at)
			 VALUES ($1, $2, $3, NULL, NOW(), NOW())
			 ON CONFLICT (cognito_sub) DO NOTHING`, sub, name, sub); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_stats (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, sub); err != nil {
			return err
		}

		return tx.Commit()
	}

	err := insert(username)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			suffix := sub
			if len(suffix) > 8 {
				suffix = suffix[:8]
			}

			err = insert(username + "_" + suffix)
		}

		if err != nil {
			return nil, err
		}
	}

	return s.GetUserByCognitoSub(ctx, sub)
}
```

(Add `errors` and `github.com/lib/pq` imports to the file.)

- [ ] **Step 6: Run store tests — expect PASS**: `go test ./internal/store/postgres/ -run 'CognitoSub' -v`

- [ ] **Step 7: Write `internal/infra/cognito_client.go`** (prod fetcher; sub == Cognito username for email-login pools):

```go
package infra

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
)

// CognitoAttributesFetcher looks up user attributes via AdminGetUser using
// the ambient AWS credential chain (instance role in prod).
type CognitoAttributesFetcher struct {
	client *cip.Client
	poolID string
}

func NewCognitoAttributesFetcher(ctx context.Context, region, poolID string) (*CognitoAttributesFetcher, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	return &CognitoAttributesFetcher{client: cip.NewFromConfig(cfg), poolID: poolID}, nil
}

// PreferredUsername returns the user's preferred_username attribute, or ""
// if absent. For email-sign-in pools the Cognito username IS the sub.
func (f *CognitoAttributesFetcher) PreferredUsername(ctx context.Context, sub string) (string, error) {
	out, err := f.client.AdminGetUser(ctx, &cip.AdminGetUserInput{
		UserPoolId: aws.String(f.poolID),
		Username:   aws.String(sub),
	})
	if err != nil {
		return "", err
	}

	for _, attr := range out.UserAttributes {
		if aws.ToString(attr.Name) == "preferred_username" {
			return aws.ToString(attr.Value), nil
		}
	}

	return "", nil
}
```

- [ ] **Step 8: Write failing service tests** in `internal/service/cognito_auth_test.go` (no AWS: local RSA key, httptest JWKS, minted tokens; store side via sqlmock is heavy here — instead the constructor takes the store, so use a real `*postgres.Store` over sqlmock with the same expectations as Step 3, or extract a tiny `userUpserter` interface if that reads cleaner — implementer's choice, but the assertions below are binding):

```go
package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// --- test scaffolding ---

type fakeFetcher struct{ name string; err error }

func (f *fakeFetcher) PreferredUsername(ctx context.Context, sub string) (string, error) {
	return f.name, f.err
}

// newJWKSServer returns (issuer URL via httptest server, private key) with
// the public JWKS served at /.well-known/jwks.json under kid "test-key".
// mintToken(priv, issuer, clientID, tokenUse, sub, exp) builds an RS256 token.
// [Implement these two helpers: generate rsa.GenerateKey(rand.Reader, 2048),
// wrap with jwk.FromRaw, set kid+alg, serve jwk.NewSet JSON via httptest;
// mint with jwt.NewBuilder().Issuer(...).Subject(...).Expiration(...).
// Claim("client_id", ...).Claim("token_use", ...).Claim("username", sub)
// signed via jwt.Sign(tok, jwt.WithKey(jwa.RS256, privJWK)).]

// --- binding test cases (table) ---
// 1. valid token, fetcher returns "alice"        -> claims{UserID: sub, Username: "alice"}, upsert called with ("sub","alice")
// 2. valid token, fetcher returns "" or error    -> upsert called with (sub, sub) — fallback, NO auth failure
// 3. expired token                               -> error
// 4. iss mismatch (other issuer, same key)       -> error
// 5. client_id mismatch                          -> error
// 6. token_use == "id"                           -> error
// 7. token signed by a DIFFERENT RSA key (kid absent from JWKS) -> error
// 8. garbage string                              -> error
// In every error case, the store/upsert must NOT be called.
```

Write the helpers plus all 8 cases as real code (the comment block above is the required coverage checklist, not the deliverable).

- [ ] **Step 9: Run — expect FAIL** (`NewCognitoAuth` undefined): `go test ./internal/service/ -run 'CognitoAuth' -v`

- [ ] **Step 10: Implement `internal/service/cognito_auth.go`**

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/rs/zerolog/log"

	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
)

// ErrInvalidToken is returned for any token that fails JWKS validation.
var ErrInvalidToken = errors.New("invalid token")

// UserAttributesFetcher looks up Cognito user attributes not present in
// access tokens (display name). Implementations must be safe for concurrent use.
type UserAttributesFetcher interface {
	PreferredUsername(ctx context.Context, sub string) (string, error)
}

// CognitoAuth validates Cognito access tokens against the pool JWKS and maps
// them to local users.
type CognitoAuth struct {
	store    *postgres.Store
	fetcher  UserAttributesFetcher
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	clientID string
}

// NewCognitoAuth fetches the JWKS once eagerly so a misconfigured pool fails
// at startup, not on the first request.
func NewCognitoAuth(ctx context.Context, store *postgres.Store, fetcher UserAttributesFetcher, issuer, clientID string) (*CognitoAuth, error) {
	jwksURL := issuer + "/.well-known/jwks.json"

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
		return nil, err
	}

	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("initial JWKS fetch %s: %w", jwksURL, err)
	}

	return &CognitoAuth{
		store:    store,
		fetcher:  fetcher,
		cache:    cache,
		jwksURL:  jwksURL,
		issuer:   issuer,
		clientID: clientID,
	}, nil
}

// ValidateToken verifies signature, issuer, client_id, token_use and expiry,
// then upserts the local user for the token's subject.
func (a *CognitoAuth) ValidateToken(ctx context.Context, tokenString string) (*AuthClaims, error) {
	keySet, err := a.cache.Get(ctx, a.jwksURL)
	if err != nil {
		return nil, err
	}

	tok, err := jwt.Parse([]byte(tokenString),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(a.issuer),
		jwt.WithClaimValue("client_id", a.clientID),
		jwt.WithClaimValue("token_use", "access"),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	sub := tok.Subject()
	if sub == "" {
		return nil, ErrInvalidToken
	}

	username, err := a.fetcher.PreferredUsername(ctx, sub)
	if err != nil || username == "" {
		// Display name is best-effort; auth never fails because of it.
		if err != nil {
			log.Warn().Err(err).Str("sub", sub).Msg("preferred_username lookup failed; falling back to sub")
		}

		username = sub
	}

	user, err := a.store.UpsertUserByCognitoSub(ctx, sub, username)
	if err != nil {
		return nil, err
	}

	return &AuthClaims{UserID: user.ID, Username: user.Username}, nil
}
```

(While `auth_service.go` still exists, `AuthClaims` is its struct — setting only `UserID`/`Username` is valid. Task 4 replaces the type definition.)

- [ ] **Step 11: Run — expect PASS**: `go test ./internal/service/ -run 'CognitoAuth' -v`, then full suite `go test ./...` (green, no regressions).

- [ ] **Step 12: Commit**

```bash
git add go.mod go.sum internal/store/postgres/ internal/service/ internal/infra/cognito_client.go
git commit -m "feat: CognitoAuth service with JWKS validation and cognito_sub user upsert"
```

---

### Task 4: Cutover — wire CognitoAuth in, delete password auth

**Files:**
- Modify: `internal/api/handler.go` (TokenValidator interface, ctx-ful authenticate; delete SignupHandler + LoginHandler)
- Modify: `internal/api/ws.go` (ctx-ful ValidateToken call)
- Modify: `cmd/server/main.go` (env config, construct CognitoAuth, delete /auth routes + JWT_SECRET)
- Modify: `internal/service/cognito_auth.go` (own the `AuthClaims` type)
- Delete: `internal/service/auth_service.go`, `internal/api/auth_handler_test.go`
- Modify: `internal/api/handler_test.go`, `internal/api/lobby_handler_test.go`, `internal/api/ws_test.go` (fake validator replaces `service.NewAuth`)
- Modify: `internal/store/postgres/user_store.go` (drop `PasswordHash` field, `CreateUser`, `GetUserByUsername`)
- Modify: `tests/e2e/e2e_test.go` (Cognito CLI auth instead of /auth endpoints)
- Modify: `go.mod` (via `go mod tidy` — golang-jwt and bcrypt usage gone)

**Interfaces:**
- Consumes: everything Task 3 produced.
- Produces: `api.TokenValidator` interface `{ ValidateToken(ctx context.Context, token string) (*service.AuthClaims, error) }`; `api.NewHandler(svc GameService, authSvc TokenValidator) *Handler`; `service.AuthClaims` is now `struct { UserID, Username string }`. Task 5 deploys exactly this binary.

- [ ] **Step 1: Redefine `AuthClaims`** — in `internal/service/cognito_auth.go` add (and delete `auth_service.go` in Step 4):

```go
// AuthClaims is the authenticated identity handed to the API layer.
type AuthClaims struct {
	UserID   string
	Username string
}
```

- [ ] **Step 2: Handler swap** in `internal/api/handler.go`:

```go
// TokenValidator authenticates bearer tokens into local user claims.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (*service.AuthClaims, error)
}
```

Change the `Handler` field to `authSvc TokenValidator`, `NewHandler(svc GameService, authSvc TokenValidator)`, and the last line of `authenticate` to `return h.authSvc.ValidateToken(r.Context(), tokenString)`. Delete `SignupHandler` and `LoginHandler` entirely (and now-unused imports).

- [ ] **Step 3: ws.go** — change the AUTH validation line to:

```go
	claims, err := h.authSvc.ValidateToken(r.Context(), authReq.Token)
```

- [ ] **Step 4: Delete old auth** — `git rm internal/service/auth_service.go internal/api/auth_handler_test.go`. In `internal/store/postgres/user_store.go` remove `PasswordHash` from `User`, and remove `CreateUser` and `GetUserByUsername` (their only callers died with `auth_service.go` — verify with `grep -rn 'CreateUser\|GetUserByUsername' --include='*.go' internal/ cmd/ tests/` and expect no hits outside `user_store.go` itself).

- [ ] **Step 5: main.go** — replace the `// 4. API` auth block:

```go
	cognitoPoolID := os.Getenv("COGNITO_POOL_ID")
	cognitoClientID := os.Getenv("COGNITO_CLIENT_ID")

	if cognitoPoolID == "" || cognitoClientID == "" {
		log.Fatalf("COGNITO_POOL_ID and COGNITO_CLIENT_ID must be set")
	}

	cognitoRegion := os.Getenv("COGNITO_REGION")
	if cognitoRegion == "" {
		cognitoRegion = "us-east-1"
	}

	ctx := context.Background()
	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cognitoRegion, cognitoPoolID)

	fetcher, err := infra.NewCognitoAttributesFetcher(ctx, cognitoRegion, cognitoPoolID)
	if err != nil {
		log.Fatalf("cognito attributes fetcher: %v", err)
	}

	authSvc, err := service.NewCognitoAuth(ctx, pgStore, fetcher, issuer, cognitoClientID)
	if err != nil {
		log.Fatalf("cognito auth: %v", err)
	}

	handler := api.NewHandler(svc, authSvc)
```

Add imports `context`, `fmt`, `github.com/joekhosbayar/go-mighty/internal/infra`. Delete the two `/auth/...` route lines and every `JWT_SECRET` reference.

- [ ] **Step 6: Test fakes** — in the api tests (`handler_test.go`, `lobby_handler_test.go`, `ws_test.go`), replace `service.NewAuth(...)`-minted tokens with a shared fake (put it in whichever of those files defines shared test helpers today; read them first):

```go
type fakeValidator struct{ claims *service.AuthClaims }

func (f *fakeValidator) ValidateToken(_ context.Context, token string) (*service.AuthClaims, error) {
	if token == "" || f.claims == nil {
		return nil, service.ErrInvalidToken
	}

	return f.claims, nil
}
```

Tests that previously logged in pass any non-empty token string with `&fakeValidator{claims: &service.AuthClaims{UserID: "...", Username: "..."}}`. Tests that asserted 401s pass an empty token or `&fakeValidator{}`.

- [ ] **Step 7: e2e rewrite** — in `tests/e2e/e2e_test.go`, replace the bodies of the signup/login step functions: create the user in the real pool and mint a token via `exec.Command("aws", "cognito-idp", ...)` using `admin-create-user` (with `preferred_username` = the unique name, `--message-action SUPPRESS`), `admin-set-user-password --permanent`, and `admin-initiate-auth --auth-flow ADMIN_USER_PASSWORD_AUTH` capturing `AuthenticationResult.AccessToken`. Pool/client ids come from env `E2E_COGNITO_POOL_ID` / `E2E_COGNITO_CLIENT_ID` (fail the suite with a clear message if unset). `a.userIDs[username]` becomes the Cognito `sub` (`admin-get-user`, attribute `sub`). Register created emails in a slice and best-effort `admin-delete-user` each in the suite teardown. Feature files don't change — only step implementations. Build-tag `integration` means `go test ./...` stays AWS-free.

- [ ] **Step 8: Tidy + full verification**

```bash
go mod tidy
go build ./... && go vet ./...
go test ./...
```

Expected: `github.com/golang-jwt/jwt/v5` gone from go.mod; build/vet clean; all unit tests green. (The integration-tagged e2e suite is exercised in Task 5 against prod config, not here.)

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat!: replace password auth with Cognito JWKS verification"
```

---

### Task 5: Deploy config, demo script, prod cutover + verification

**Files:**
- Modify: `deploy/compose/remote-deploy.sh` (render Cognito env into `.env`, drop JWT_SECRET)
- Modify: `docker-compose.yml` (dev: Cognito env passthrough, drop JWT_SECRET)
- Modify: `scripts/demo_game_flow.sh` (Cognito CLI auth)

**Interfaces:**
- Consumes: SSM params from Task 1, binary behavior from Task 4, Plan-1 deploy pipeline (`deploy/scripts/deploy.sh`, ECR image build/push from repo HEAD).
- Produces: production running Cognito-only auth; demo script working end-to-end against prod.

- [ ] **Step 1: remote-deploy.sh** — in the `.env` heredoc, delete the `JWT_SECRET=...` line and add:

```bash
COGNITO_POOL_ID=$(param /mighty/cognito_pool_id)
COGNITO_CLIENT_ID=$(param /mighty/cognito_client_id)
COGNITO_REGION=us-east-1
```

- [ ] **Step 2: dev docker-compose.yml** — in the `mighty` service `environment:` block, delete `JWT_SECRET: ...` and add:

```yaml
      COGNITO_POOL_ID: "${COGNITO_POOL_ID:-}"
      COGNITO_CLIENT_ID: "${COGNITO_CLIENT_ID:-}"
      COGNITO_REGION: "${COGNITO_REGION:-us-east-1}"
```

(Local dev now points at the real pool; the server fails fast at startup if the vars are unset. Without AWS creds in the container the display-name lookup falls back to sub — documented behavior.)

- [ ] **Step 3: demo_game_flow.sh** — replace the register/login loop (steps 0 in the script) with Cognito CLI auth; the rest of the script is untouched:

```bash
POOL_ID="${POOL_ID:?set POOL_ID (tofu output -raw cognito_pool_id)}"
CLIENT_ID="${CLIENT_ID:?set CLIENT_ID (tofu output -raw cognito_client_id)}"

for i in "${!USERS[@]}"; do
  USERNAME="${USERS[$i]}_$(date +%s)"
  PASSWORD="MightyDemo1"
  EMAIL="${USERNAME}@example.com"

  echo "Creating Cognito user ${USERNAME}..."
  aws cognito-idp admin-create-user --region us-east-1 --user-pool-id "$POOL_ID" \
    --username "$EMAIL" --message-action SUPPRESS \
    --user-attributes Name=email,Value="$EMAIL" Name=email_verified,Value=true Name=preferred_username,Value="$USERNAME" >/dev/null
  aws cognito-idp admin-set-user-password --region us-east-1 --user-pool-id "$POOL_ID" \
    --username "$EMAIL" --password "$PASSWORD" --permanent

  TOKEN=$(aws cognito-idp admin-initiate-auth --region us-east-1 --user-pool-id "$POOL_ID" \
    --client-id "$CLIENT_ID" --auth-flow ADMIN_USER_PASSWORD_AUTH \
    --auth-parameters USERNAME="$EMAIL",PASSWORD="$PASSWORD" \
    --query 'AuthenticationResult.AccessToken' --output text)
  TOKENS+=("$TOKEN")

  USER_ID=$(aws cognito-idp admin-get-user --region us-east-1 --user-pool-id "$POOL_ID" \
    --username "$EMAIL" --query "UserAttributes[?Name=='sub'].Value" --output text)
  USER_IDS+=("$USER_ID")
done
```

- [ ] **Step 4: Build + push the new image, deploy**

```bash
ECR_URL=$(tofu -chdir=deploy/terraform output -raw ecr_repo_url)
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin "${ECR_URL%%/*}"
docker buildx build --platform linux/arm64 -t "${ECR_URL}:latest" --push .
./deploy/scripts/deploy.sh
```

Expected: deploy Success; `migrate` runs `000002` (log line `2/u cognito_cutover`); app starts (its eager JWKS fetch proves pool config; a crash-loop here means bad COGNITO_* env — check `docker compose logs mighty` via SSM).

- [ ] **Step 5: Prod verification — negative first**

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://api.themighty.gg/games            # no token
curl -s -o /dev/null -w '%{http_code}\n' -H 'Authorization: Bearer garbage' https://api.themighty.gg/games
```

Expected: non-200 (401/500-class per existing handler behavior — record actual) for both; and `/healthz` still 200.

- [ ] **Step 6: Prod verification — positive end-to-end**

```bash
POOL_ID=$(tofu -chdir=deploy/terraform output -raw cognito_pool_id) \
CLIENT_ID=$(tofu -chdir=deploy/terraform output -raw cognito_client_id) \
BASE_URL=https://api.themighty.gg ./scripts/demo_game_flow.sh
```

Expected: full flow — 5 Cognito users created, game created/joined/dealt, bid accepted. Then prove the WSS AUTH path with a real Cognito token: connect `npx -y wscat -c "wss://api.themighty.gg/games/<game-id-from-script>/ws"`, send `{"type":"AUTH","token":"<a token the script printed>"}` — expect no `unauthorized` error (connection stays open / receives events).

- [ ] **Step 7: Verify display names landed** (the AdminGetUser path, not the sub fallback)

```bash
# via SSM on the instance:
docker exec $(docker ps -qf name=postgres) psql -U postgres -c "SELECT username, cognito_sub FROM users ORDER BY created_at DESC LIMIT 5;"
```

Expected: `username` values like `alice_1752...` (the preferred_username), NOT bare UUIDs.

- [ ] **Step 8: Commit**

```bash
git add deploy/compose/remote-deploy.sh docker-compose.yml scripts/demo_game_flow.sh
git commit -m "feat: deploy Cognito auth config; demo script uses Cognito CLI users"
```

---

## Done criteria (whole plan)

- Pool + SPA client live; CLI-issued access token decodes with correct `iss`/`client_id`/`token_use`.
- `go test ./...` green with zero references to bcrypt/golang-jwt/JWT_SECRET; `/auth/*` routes gone.
- Prod rejects missing/garbage tokens, accepts real Cognito tokens over REST and WSS, and stores human display names via the AdminGetUser path.
- Migration `000002` applied in prod (users table has `cognito_sub`, no `password_hash`).

## Explicitly deferred

- Frontend auth UI, passkey/TOTP/reset flows in the app (Plan 3 — the pool already supports them).
- Removing `ALLOW_ADMIN_USER_PASSWORD_AUTH` from the SPA client (after Plan 3 ships real signup).
- Per-user rate limiting keyed on sub (Plan 4).
