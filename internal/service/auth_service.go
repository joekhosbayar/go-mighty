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
	ErrUserAlreadyExists  = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type AuthService struct {
	store     *postgres.Store
	jwtSecret []byte
}

type AuthClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func NewAuthService(store *postgres.Store, jwtSecret string) *AuthService {
	return &AuthService{
		store:     store,
		jwtSecret: []byte(jwtSecret),
	}
}

func (s *AuthService) Signup(ctx context.Context, username, password, email string) (*postgres.User, error) {
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

func (s *AuthService) Login(ctx context.Context, username, password string) (string, error) {
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

func (s *AuthService) ValidateToken(tokenString string) (*AuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AuthClaims{}, func(token *jwt.Token) (interface{}, error) {
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
