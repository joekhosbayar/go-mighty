package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"

	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
)

var errTestFetch = errors.New("fetch failed")

// --- test scaffolding ---

type fakeFetcher struct {
	name string
	err  error
}

func (f *fakeFetcher) PreferredUsername(ctx context.Context, sub string) (string, error) {
	return f.name, f.err
}

// fakeStore is the userUpserter test double: it records whether/how it was
// called so error-path tests can assert the upsert never happens.
type fakeStore struct {
	called      bool
	gotSub      string
	gotUsername string
	user        *postgres.User
	err         error
}

func (f *fakeStore) UpsertUserByCognitoSub(ctx context.Context, sub, username string) (*postgres.User, error) {
	f.called = true
	f.gotSub = sub
	f.gotUsername = username

	if f.err != nil {
		return nil, f.err
	}

	if f.user != nil {
		return f.user, nil
	}

	return &postgres.User{ID: sub, Username: username}, nil
}

// newJWKSServer returns (issuer URL via httptest server, private key) with
// the public JWKS served under kid "test-key".
func newJWKSServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	pubJWK, err := jwk.FromRaw(priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := pubJWK.Set(jwk.KeyIDKey, "test-key"); err != nil {
		t.Fatal(err)
	}
	if err := pubJWK.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		t.Fatal(err)
	}

	set := jwk.NewSet()
	if err := set.AddKey(pubJWK); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(server.Close)

	return server, priv
}

// mintToken builds and signs an RS256 token with the given claims, keyed
// under kid "test-key" (matching newJWKSServer's published key).
func mintToken(t *testing.T, priv *rsa.PrivateKey, issuer, clientID, tokenUse, sub string, exp time.Time) string {
	t.Helper()
	return mintTokenWithKid(t, priv, "test-key", issuer, clientID, tokenUse, sub, exp)
}

func mintTokenWithKid(t *testing.T, priv *rsa.PrivateKey, kid, issuer, clientID, tokenUse, sub string, exp time.Time) string {
	t.Helper()

	privJWK, err := jwk.FromRaw(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := privJWK.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatal(err)
	}
	if err := privJWK.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		t.Fatal(err)
	}

	tok, err := jwt.NewBuilder().
		Issuer(issuer).
		Subject(sub).
		Expiration(exp).
		Claim("client_id", clientID).
		Claim("token_use", tokenUse).
		Claim("username", sub).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privJWK))
	if err != nil {
		t.Fatal(err)
	}

	return string(signed)
}

// newTestAuth builds a CognitoAuth pointed at a local JWKS server, with the
// given store/fetcher doubles. The returned cancel MUST be deferred by the
// caller so the JWKS cache's background workers stop (goleak).
func newTestAuth(t *testing.T, issuer string, store userUpserter, fetcher UserAttributesFetcher) *CognitoAuth {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	auth, err := newCognitoAuth(ctx, store, fetcher, issuer, "test-client-id")
	if err != nil {
		t.Fatalf("newCognitoAuth: %v", err)
	}

	return auth
}

const testClientID = "test-client-id"

// --- binding test cases ---

// 1. valid token, fetcher returns "alice" -> claims{UserID: sub, Username: "alice"}, upsert called with (sub, "alice")
func TestValidateToken_ValidToken_FetcherReturnsName(t *testing.T) {
	server, priv := newJWKSServer(t)
	sub := "sub-abc"
	token := mintToken(t, priv, server.URL, testClientID, "access", sub, time.Now().Add(time.Hour))

	store := &fakeStore{}
	fetcher := &fakeFetcher{name: "alice"}
	auth := newTestAuth(t, server.URL, store, fetcher)

	claims, err := auth.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.UserID != sub || claims.Username != "alice" {
		t.Fatalf("got claims %+v", claims)
	}
	if !store.called || store.gotSub != sub || store.gotUsername != "alice" {
		t.Fatalf("store not called as expected: %+v", store)
	}
}

// 2. valid token, fetcher returns "" or error -> upsert called with (sub, sub) fallback, no auth failure
func TestValidateToken_ValidToken_FetcherFallback(t *testing.T) {
	cases := []struct {
		name    string
		fetcher *fakeFetcher
	}{
		{name: "empty_username", fetcher: &fakeFetcher{name: ""}},
		{name: "fetcher_error", fetcher: &fakeFetcher{name: "", err: errTestFetch}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, priv := newJWKSServer(t)
			sub := "sub-fallback"
			token := mintToken(t, priv, server.URL, testClientID, "access", sub, time.Now().Add(time.Hour))

			store := &fakeStore{}
			auth := newTestAuth(t, server.URL, store, tc.fetcher)

			claims, err := auth.ValidateToken(context.Background(), token)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if claims.UserID != sub || claims.Username != sub {
				t.Fatalf("got claims %+v", claims)
			}
			if !store.called || store.gotSub != sub || store.gotUsername != sub {
				t.Fatalf("store not called with fallback username: %+v", store)
			}
		})
	}
}

// 3. expired token -> error
func TestValidateToken_ExpiredToken(t *testing.T) {
	server, priv := newJWKSServer(t)
	sub := "sub-expired"
	token := mintToken(t, priv, server.URL, testClientID, "access", sub, time.Now().Add(-time.Hour))

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err := auth.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}

// 4. iss mismatch (other issuer, same key) -> error
func TestValidateToken_IssuerMismatch(t *testing.T) {
	server, priv := newJWKSServer(t)
	sub := "sub-iss-mismatch"
	token := mintToken(t, priv, "https://other-issuer.example.com", testClientID, "access", sub, time.Now().Add(time.Hour))

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err := auth.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for issuer mismatch")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}

// 5. client_id mismatch -> error
func TestValidateToken_ClientIDMismatch(t *testing.T) {
	server, priv := newJWKSServer(t)
	sub := "sub-cid-mismatch"
	token := mintToken(t, priv, server.URL, "wrong-client-id", "access", sub, time.Now().Add(time.Hour))

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err := auth.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for client_id mismatch")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}

// 6. token_use == "id" -> error
func TestValidateToken_TokenUseID(t *testing.T) {
	server, priv := newJWKSServer(t)
	sub := "sub-id-token"
	token := mintToken(t, priv, server.URL, testClientID, "id", sub, time.Now().Add(time.Hour))

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err := auth.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for token_use=id")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}

// 7. token signed by a DIFFERENT RSA key (kid absent from JWKS) -> error
func TestValidateToken_UnknownSigningKey(t *testing.T) {
	server, _ := newJWKSServer(t)

	otherPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	sub := "sub-unknown-key"
	token := mintTokenWithKid(t, otherPriv, "other-key", server.URL, testClientID, "access", sub, time.Now().Add(time.Hour))

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err = auth.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for unknown signing key")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}

// 8b. valid iss/client_id/token_use/sub but NO expiration claim -> error
// (defense-in-depth: an absent exp must not be treated as "never expires")
func TestValidateToken_MissingExpiration(t *testing.T) {
	server, priv := newJWKSServer(t)
	sub := "sub-no-exp"

	privJWK, err := jwk.FromRaw(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := privJWK.Set(jwk.KeyIDKey, "test-key"); err != nil {
		t.Fatal(err)
	}
	if err := privJWK.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		t.Fatal(err)
	}

	tok, err := jwt.NewBuilder().
		Issuer(server.URL).
		Subject(sub).
		Claim("client_id", testClientID).
		Claim("token_use", "access").
		Claim("username", sub).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privJWK))
	if err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err = auth.ValidateToken(context.Background(), string(signed))
	if err == nil {
		t.Fatal("expected error for token missing exp claim")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}

// 8. garbage string -> error
func TestValidateToken_GarbageString(t *testing.T) {
	server, _ := newJWKSServer(t)

	store := &fakeStore{}
	auth := newTestAuth(t, server.URL, store, &fakeFetcher{name: "alice"})

	_, err := auth.ValidateToken(context.Background(), "this-is-not-a-jwt")
	if err == nil {
		t.Fatal("expected error for garbage token string")
	}
	if store.called {
		t.Fatal("store must not be called on validation failure")
	}
}
