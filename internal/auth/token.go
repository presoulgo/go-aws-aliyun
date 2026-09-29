package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the JWT claims issued at login.
type Claims struct {
	UserID  uint   `json:"uid"`
	Role    string `json:"role"`
	Version int    `json:"ver"`
	jwt.RegisteredClaims
}

// Tokens signs and verifies HS256 login tokens.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokens creates a token signer.
func NewTokens(secret []byte, ttl time.Duration) *Tokens {
	return &Tokens{secret: secret, ttl: ttl, now: time.Now}
}

// Issue creates a token for the user.
func (t *Tokens) Issue(userID uint, username, role string, version int) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := Claims{
		UserID:  userID,
		Role:    role,
		Version: version,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			Issuer:    "yunshu",
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	return signed, exp, err
}

// ErrInvalidToken is returned for malformed, expired or forged tokens.
var ErrInvalidToken = errors.New("登录已失效，请重新登录")

// Parse verifies a token and returns its claims.
func (t *Tokens) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
		return t.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("yunshu"),
		jwt.WithTimeFunc(t.now),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid || claims.UserID == 0 {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
