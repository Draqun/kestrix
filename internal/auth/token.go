package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"kestrix/internal/domain"
)

// TokenTTL is how long an issued token remains valid. See design.md D7: no
// refresh token, so this bounds how long a revoked or deleted account keeps
// working access.
const TokenTTL = time.Hour

// ErrInvalidToken is returned by JWTIssuer.Verify for any token that fails
// signature, algorithm, or expiry checks. The specific cause is not exposed
// to callers beyond this sentinel, so an authorization failure never
// discloses more than "not accepted".
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// jwtClaims is the on-the-wire claim set. Role travels as a private claim
// alongside the registered subject and expiry.
type jwtClaims struct {
	Role domain.Role `json:"role"`
	jwt.RegisteredClaims
}

// JWTIssuer implements domain.TokenIssuer with HMAC-SHA256 signing. The key
// is supplied by a KeyFunc so it can be reloaded without reconstructing the
// issuer; FileSigningKeyProvider.Key is a typical KeyFunc-compatible source.
type JWTIssuer struct {
	// Key returns the current signing key. It is called on every Issue and
	// Verify, so a provider may re-read its backing file each time.
	Key func() ([]byte, error)
}

var _ domain.TokenIssuer = JWTIssuer{}

// Issue signs a token carrying user.Name and user.Role, valid for TokenTTL.
func (j JWTIssuer) Issue(user domain.User) (string, time.Time, error) {
	key, err := j.Key()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	expiresAt := now.Add(TokenTTL)
	claims := jwtClaims{
		Role: user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.Name,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify parses and validates tokenString, requiring HS256 as the signing
// algorithm - an "alg" of "none" or any asymmetric algorithm is refused
// before any claim is read, closing the classic algorithm-confusion attack.
// A verification, algorithm, or expiry failure returns ErrInvalidToken.
func (j JWTIssuer) Verify(tokenString string) (domain.Claims, error) {
	key, err := j.Key()
	if err != nil {
		return domain.Claims{}, err
	}
	var claims jwtClaims
	_, err = jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (interface{}, error) {
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return domain.Claims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return domain.Claims{
		UserName:  claims.Subject,
		Role:      claims.Role,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}
