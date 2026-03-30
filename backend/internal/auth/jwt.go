package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"k8s-proxmox-manager/backend/internal/rbac"
)

type TokenService struct {
	secret []byte
	ttl    time.Duration
}

type Claims struct {
	Role rbac.Role `json:"role"`
	jwt.RegisteredClaims
}

func NewTokenService(secret string, ttlMinutes int) *TokenService {
	if ttlMinutes <= 0 {
		ttlMinutes = 120
	}
	return &TokenService{
		secret: []byte(secret),
		ttl:    time.Duration(ttlMinutes) * time.Minute,
	}
}

func (s *TokenService) Issue(username string, role rbac.Role) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *TokenService) Parse(tokenRaw string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenRaw, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
