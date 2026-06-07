// Package service provides the business logic for the Mighty application, including
// authentication and game management services.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrUserAlreadyExists is returned when a user attempts to sign up with an existing username.
	ErrUserAlreadyExists = errors.New("user already exists")
	// ErrInvalidCredentials is returned when login fails due to incorrect username or password.
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// Auth provides authentication services, including signup, login, and token validation.
type Auth struct {
	store     *postgres.Store
	jwtSecret []byte
}

// AuthClaims represents the custom JWT claims used for authentication.
type AuthClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// NewAuth creates and returns a new Auth service instance.
func NewAuth(store *postgres.Store, jwtSecret string) *Auth {
	return &Auth{
		store:     store,
		jwtSecret: []byte(jwtSecret),
	}
}

// Signup creates a new user account.
func (s *Auth) Signup(ctx context.Context, username, password, email string) (*postgres.User, error) {
	existing, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		return nil, ErrUserAlreadyExists
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &postgres.User{
		ID:           uuid.New().String(),
		Username:     username,
		PasswordHash: string(hashedPassword),
		Email:        email,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrUserAlreadyExists
		}

		return nil, err
	}

	return user, nil
}

// Login authenticates a user and returns a JWT token.
func (s *Auth) Login(ctx context.Context, username, password string) (string, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return "", err
	}

	if user == nil {
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	claims := &AuthClaims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(s.jwtSecret)
}

// ValidateToken parses and validates a JWT token string.
func (s *Auth) ValidateToken(tokenString string) (*AuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AuthClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}

		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*AuthClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
