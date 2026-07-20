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

// AuthClaims is the authenticated identity handed to the API layer.
type AuthClaims struct {
	UserID   string
	Username string
}

// UserAttributesFetcher looks up Cognito user attributes not present in
// access tokens (display name). Implementations must be safe for concurrent use.
type UserAttributesFetcher interface {
	PreferredUsername(ctx context.Context, sub string) (string, error)
}

// userUpserter is the store dependency CognitoAuth actually needs. It exists
// so tests can substitute a lightweight fake instead of a *postgres.Store
// backed by sqlmock; *postgres.Store satisfies it trivially, and the public
// NewCognitoAuth constructor still takes a concrete *postgres.Store per the
// interface Task 4 wires in.
type userUpserter interface {
	UpsertUserByCognitoSub(ctx context.Context, sub, username string) (*postgres.User, error)
}

// CognitoAuth validates Cognito access tokens against the pool JWKS and maps
// them to local users.
type CognitoAuth struct {
	store    userUpserter
	fetcher  UserAttributesFetcher
	cache    *jwk.Cache
	jwksURL  string
	issuer   string
	clientID string
}

// NewCognitoAuth fetches the JWKS once eagerly so a misconfigured pool fails
// at startup, not on the first request.
func NewCognitoAuth(ctx context.Context, store *postgres.Store, fetcher UserAttributesFetcher, issuer, clientID string) (*CognitoAuth, error) {
	return newCognitoAuth(ctx, store, fetcher, issuer, clientID)
}

// newCognitoAuth is the unexported constructor accepting the userUpserter
// interface directly, allowing service tests to inject a fake store without
// going through sqlmock.
func newCognitoAuth(ctx context.Context, store userUpserter, fetcher UserAttributesFetcher, issuer, clientID string) (*CognitoAuth, error) {
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
		jwt.WithRequiredClaim("exp"),
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
