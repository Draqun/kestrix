package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"kestrix/internal/domain"
)

func fixedKey() ([]byte, error) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key, nil
}

func TestJWTIssuer_IssueAndVerify(t *testing.T) {
	issuer := JWTIssuer{Key: fixedKey}
	user := domain.User{Name: "alice", Role: domain.RoleOperator}

	token, expiresAt, err := issuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("Issue returned empty token")
	}
	if remaining := time.Until(expiresAt); remaining > TokenTTL || remaining < TokenTTL-time.Second {
		t.Fatalf("expiresAt = %v, want ~%v from now", expiresAt, TokenTTL)
	}

	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UserName != "alice" || claims.Role != domain.RoleOperator {
		t.Fatalf("claims = %+v, want alice/operator", claims)
	}
}

func TestJWTIssuer_IssueAndVerify_RoundTripsTokenVersion(t *testing.T) {
	issuer := JWTIssuer{Key: fixedKey}
	user := domain.User{Name: "alice", Role: domain.RoleOperator, TokenVersion: 7}

	token, _, err := issuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.TokenVersion != 7 {
		t.Fatalf("claims.TokenVersion = %d, want 7", claims.TokenVersion)
	}
}

func TestJWTIssuer_Verify_RejectsTokenWithNoVersionClaim(t *testing.T) {
	key, _ := fixedKey()
	// Built without a ver field at all, distinct from a token that carries
	// ver: 0 - jwt.MapClaims omits an unset key rather than defaulting it.
	claims := jwt.MapClaims{
		"sub":  "legacy-user",
		"role": string(domain.RoleViewer),
		"iat":  jwt.NewNumericDate(time.Now()),
		"exp":  jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	issuer := JWTIssuer{Key: fixedKey}
	if _, err := issuer.Verify(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(no ver claim) error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTIssuer_Verify_AcceptsExplicitZeroVersion(t *testing.T) {
	// ver: 0 is a present-but-zero claim, distinct from an absent one, and
	// must decode to TokenVersion 0 rather than being treated as missing.
	key, _ := fixedKey()
	zero := 0
	claims := jwtClaims{
		Role: domain.RoleViewer,
		Ver:  &zero,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "zero-version-user",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	issuer := JWTIssuer{Key: fixedKey}
	got, err := issuer.Verify(signed)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.TokenVersion != 0 {
		t.Fatalf("claims.TokenVersion = %d, want 0 (present, not absent)", got.TokenVersion)
	}
}

func TestJWTIssuer_Verify_Expired(t *testing.T) {
	key, _ := fixedKey()
	claims := jwtClaims{
		Role: domain.RoleViewer,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "bob",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	issuer := JWTIssuer{Key: fixedKey}
	if _, err := issuer.Verify(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(expired) error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTIssuer_Verify_WrongKey(t *testing.T) {
	issuer := JWTIssuer{Key: fixedKey}
	user := domain.User{Name: "carol", Role: domain.RoleAdmin}
	token, _, err := issuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	otherIssuer := JWTIssuer{Key: func() ([]byte, error) {
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(255 - i)
		}
		return key, nil
	}}
	if _, err := otherIssuer.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(wrong key) error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTIssuer_Verify_RejectsNoneAlgorithm(t *testing.T) {
	claims := jwtClaims{
		Role: domain.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "attacker",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	unsafeSigned, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none-alg token: %v", err)
	}

	issuer := JWTIssuer{Key: fixedKey}
	if _, err := issuer.Verify(unsafeSigned); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(alg=none) error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTIssuer_Verify_RejectsMalformedToken(t *testing.T) {
	issuer := JWTIssuer{Key: fixedKey}
	if _, err := issuer.Verify("not-a-jwt-at-all"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(garbage) error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTIssuer_TokenSurvivesRestart(t *testing.T) {
	// Simulates a restart by constructing a second issuer from the same
	// key source: a token issued before must still verify after.
	before := JWTIssuer{Key: fixedKey}
	token, _, err := before.Issue(domain.User{Name: "dana", Role: domain.RoleViewer})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	after := JWTIssuer{Key: fixedKey}
	claims, err := after.Verify(token)
	if err != nil {
		t.Fatalf("Verify after restart: %v", err)
	}
	if claims.UserName != "dana" {
		t.Fatalf("claims.UserName = %q, want dana", claims.UserName)
	}
}

func TestJWTIssuer_Verify_KeyUnavailable(t *testing.T) {
	issuer := JWTIssuer{Key: func() ([]byte, error) {
		return nil, ErrSigningKeyUnavailable
	}}
	if _, err := issuer.Verify("any-token"); !errors.Is(err, ErrSigningKeyUnavailable) {
		t.Fatalf("Verify error = %v, want ErrSigningKeyUnavailable", err)
	}
}

func TestJWTIssuer_Issue_KeyUnavailable(t *testing.T) {
	issuer := JWTIssuer{Key: func() ([]byte, error) {
		return nil, ErrSigningKeyUnavailable
	}}
	if _, _, err := issuer.Issue(domain.User{Name: "eve", Role: domain.RoleViewer}); !errors.Is(err, ErrSigningKeyUnavailable) {
		t.Fatalf("Issue error = %v, want ErrSigningKeyUnavailable", err)
	}
}
