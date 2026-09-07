package service

import (
	"errors"
	"testing"

	"kestrix/internal/domain"
	"kestrix/internal/mocks"
)

func TestAuthService_Login_TokenIssuanceFails(t *testing.T) {
	users := mocks.NewMockUserStore()
	hasher := &mocks.MockPasswordHasher{}
	hash, err := hasher.Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if _, err := users.CreateUser("alice", domain.RoleOperator, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tokens := mocks.NewMockTokenIssuer()
	tokens.IssueErr = errors.New("signing key unavailable")

	auth := NewAuthService(users, hasher, tokens)
	_, _, err = auth.Login("alice", "s3cret")
	if err == nil {
		t.Fatal("Login: expected an error when token issuance fails")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want a token-issuance error, not ErrInvalidCredentials", err)
	}
}

func newTestAuthService(t *testing.T) (*AuthService, *mocks.MockUserStore, *mocks.MockPasswordHasher) {
	t.Helper()
	users := mocks.NewMockUserStore()
	hasher := &mocks.MockPasswordHasher{}
	tokens := mocks.NewMockTokenIssuer()
	hash, err := hasher.Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if _, err := users.CreateUser("alice", domain.RoleOperator, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return NewAuthService(users, hasher, tokens), users, hasher
}

func TestAuthService_Login_Success(t *testing.T) {
	auth, _, _ := newTestAuthService(t)
	token, expiresAt, err := auth.Login("alice", "s3cret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("Login returned empty token")
	}
	if expiresAt.IsZero() {
		t.Fatal("Login returned zero expiry")
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	auth, _, _ := newTestAuthService(t)
	_, _, err := auth.Login("alice", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthService_Login_UnknownAccount(t *testing.T) {
	auth, _, _ := newTestAuthService(t)
	_, _, err := auth.Login("nobody", "anything")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(unknown account) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthService_Login_BothFailureModesReturnIdenticalError(t *testing.T) {
	auth, _, _ := newTestAuthService(t)
	_, _, wrongPasswordErr := auth.Login("alice", "wrong-password")
	_, _, unknownAccountErr := auth.Login("nobody", "anything")
	if wrongPasswordErr.Error() != unknownAccountErr.Error() {
		t.Fatalf("errors differ: wrong-password=%q unknown-account=%q", wrongPasswordErr, unknownAccountErr)
	}
}

func TestAuthService_Login_UnknownAccountStillHashes(t *testing.T) {
	auth, _, hasher := newTestAuthService(t)
	before := hasher.VerifyCalls
	_, _, err := auth.Login("nobody", "anything")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if hasher.VerifyCalls != before+1 {
		t.Fatalf("VerifyCalls = %d, want %d (dummy verification must still run)", hasher.VerifyCalls, before+1)
	}
}
