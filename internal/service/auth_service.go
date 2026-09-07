package service

import (
	"errors"
	"fmt"
	"time"

	"kestrix/internal/domain"
)

// ErrInvalidCredentials is returned by AuthService.Login for both a wrong
// password and an unknown account, so a caller cannot distinguish which
// one occurred - see openspec spec user-authentication, "Token issuance on
// login".
var ErrInvalidCredentials = errors.New("invalid name or password")

// AuthService authenticates users and issues tokens. It composes the three
// narrow ports from design.md D2 so an OIDC provider can later replace the
// PasswordHasher usage without touching UserStore or TokenIssuer.
type AuthService struct {
	Users  domain.UserStore
	Hasher domain.PasswordHasher
	Tokens domain.TokenIssuer
}

// NewAuthService constructs an AuthService from its three ports.
func NewAuthService(users domain.UserStore, hasher domain.PasswordHasher, tokens domain.TokenIssuer) *AuthService {
	return &AuthService{Users: users, Hasher: hasher, Tokens: tokens}
}

// dummyVerifier is satisfied by Argon2Hasher without internal/service
// importing internal/auth, keeping the hexagonal layering intact: service
// depends on domain.PasswordHasher, not on any concrete implementation.
type dummyVerifier interface {
	VerifyDummy(password string)
}

// Login verifies name and password and, on success, issues a token. A wrong
// password and an unknown name both return ErrInvalidCredentials, and both
// perform equivalent hashing work, so neither the error nor response timing
// discloses which one occurred or whether the account exists.
func (s *AuthService) Login(name, password string) (token string, expiresAt time.Time, err error) {
	cred, err := s.Users.GetUserCredential(name)
	if err != nil {
		if dv, ok := s.Hasher.(dummyVerifier); ok {
			dv.VerifyDummy(password)
		}
		return "", time.Time{}, ErrInvalidCredentials
	}
	ok, err := s.Hasher.Verify(cred.PasswordHash, password)
	if err != nil || !ok {
		return "", time.Time{}, ErrInvalidCredentials
	}
	token, expiresAt, err = s.Tokens.Issue(cred.User)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("issue token: %w", err)
	}
	return token, expiresAt, nil
}
