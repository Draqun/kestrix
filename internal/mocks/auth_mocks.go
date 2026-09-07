package mocks

import (
	"fmt"
	"sync"
	"time"

	"kestrix/internal/domain"
	"kestrix/internal/store"
)

// MockUserStore is an in-memory domain.UserStore for unit tests.
type MockUserStore struct {
	mu    sync.Mutex
	Users map[string]domain.UserCredential

	CreateErr error
	GetErr    error
}

// NewMockUserStore returns an empty store ready for use.
func NewMockUserStore() *MockUserStore {
	return &MockUserStore{Users: make(map[string]domain.UserCredential)}
}

func (m *MockUserStore) CreateUser(name string, role domain.Role, passwordHash string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateErr != nil {
		return domain.User{}, m.CreateErr
	}
	if _, exists := m.Users[name]; exists {
		return domain.User{}, fmt.Errorf("%w: %s", store.ErrUserExists, name)
	}
	now := time.Now().UTC()
	u := domain.User{Name: name, Role: role, CreatedAt: now, UpdatedAt: now}
	m.Users[name] = domain.UserCredential{User: u, PasswordHash: passwordHash}
	return u, nil
}

func (m *MockUserStore) GetUser(name string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.GetErr != nil {
		return domain.User{}, m.GetErr
	}
	uc, ok := m.Users[name]
	if !ok {
		return domain.User{}, fmt.Errorf("%w: %s", store.ErrUserNotFound, name)
	}
	return uc.User, nil
}

func (m *MockUserStore) GetUserCredential(name string) (domain.UserCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.GetErr != nil {
		return domain.UserCredential{}, m.GetErr
	}
	uc, ok := m.Users[name]
	if !ok {
		return domain.UserCredential{}, fmt.Errorf("%w: %s", store.ErrUserNotFound, name)
	}
	return uc, nil
}

func (m *MockUserStore) ListUsers() ([]domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.User, 0, len(m.Users))
	for _, uc := range m.Users {
		out = append(out, uc.User)
	}
	return out, nil
}

func (m *MockUserStore) UpdateUserRole(name string, role domain.Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	uc, ok := m.Users[name]
	if !ok {
		return fmt.Errorf("%w: %s", store.ErrUserNotFound, name)
	}
	uc.Role = role
	uc.UpdatedAt = time.Now().UTC()
	m.Users[name] = uc
	return nil
}

func (m *MockUserStore) UpdateUserPassword(name string, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	uc, ok := m.Users[name]
	if !ok {
		return fmt.Errorf("%w: %s", store.ErrUserNotFound, name)
	}
	uc.PasswordHash = passwordHash
	uc.UpdatedAt = time.Now().UTC()
	m.Users[name] = uc
	return nil
}

func (m *MockUserStore) DeleteUser(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Users[name]; !ok {
		return fmt.Errorf("%w: %s", store.ErrUserNotFound, name)
	}
	delete(m.Users, name)
	return nil
}

// MockGrantStore is an in-memory domain.GrantStore for unit tests.
type MockGrantStore struct {
	mu     sync.Mutex
	Grants map[[2]string]domain.Role
}

// NewMockGrantStore returns an empty store ready for use.
func NewMockGrantStore() *MockGrantStore {
	return &MockGrantStore{Grants: make(map[[2]string]domain.Role)}
}

func (m *MockGrantStore) SetProjectGrant(userName, projectName string, role domain.Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Grants[[2]string{userName, projectName}] = role
	return nil
}

func (m *MockGrantStore) RemoveProjectGrant(userName, projectName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := [2]string{userName, projectName}
	if _, ok := m.Grants[key]; !ok {
		return fmt.Errorf("%w: %s/%s", store.ErrGrantNotFound, userName, projectName)
	}
	delete(m.Grants, key)
	return nil
}

func (m *MockGrantStore) ListGrantsForUser(userName string) ([]domain.ProjectGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.ProjectGrant
	for k, role := range m.Grants {
		if k[0] == userName {
			out = append(out, domain.ProjectGrant{UserName: k[0], ProjectName: k[1], Role: role})
		}
	}
	return out, nil
}

func (m *MockGrantStore) ListGrantsForProject(projectName string) ([]domain.ProjectGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.ProjectGrant
	for k, role := range m.Grants {
		if k[1] == projectName {
			out = append(out, domain.ProjectGrant{UserName: k[0], ProjectName: k[1], Role: role})
		}
	}
	return out, nil
}

func (m *MockGrantStore) ResolveGrant(userName, projectName string) (domain.Role, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	role, ok := m.Grants[[2]string{userName, projectName}]
	return role, ok, nil
}

// MockPasswordHasher is a domain.PasswordHasher stand-in that treats the hash
// as the password itself, prefixed so an unhashed password never accidentally
// verifies. VerifyCalls counts calls, including from VerifyDummy, so a test
// can assert dummy verification actually ran.
type MockPasswordHasher struct {
	mu          sync.Mutex
	VerifyCalls int
	HashErr     error
}

func (h *MockPasswordHasher) Hash(password string) (string, error) {
	if h.HashErr != nil {
		return "", h.HashErr
	}
	return "mock-hash:" + password, nil
}

func (h *MockPasswordHasher) Verify(hash, password string) (bool, error) {
	h.mu.Lock()
	h.VerifyCalls++
	h.mu.Unlock()
	return hash == "mock-hash:"+password, nil
}

// VerifyDummy satisfies the optional dummyVerifier interface AuthService
// looks for, so tests can assert it ran for an unknown account.
func (h *MockPasswordHasher) VerifyDummy(password string) {
	h.mu.Lock()
	h.VerifyCalls++
	h.mu.Unlock()
}

// MockTokenIssuer is a domain.TokenIssuer stand-in that "issues" a
// deterministic token string and looks it up on Verify, avoiding any real
// signing in unit tests that only need identity plumbing.
type MockTokenIssuer struct {
	mu     sync.Mutex
	issued map[string]domain.Claims
	next   int

	IssueErr  error
	VerifyErr error
}

// NewMockTokenIssuer returns an issuer ready for use.
func NewMockTokenIssuer() *MockTokenIssuer {
	return &MockTokenIssuer{issued: make(map[string]domain.Claims)}
}

func (m *MockTokenIssuer) Issue(user domain.User) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.IssueErr != nil {
		return "", time.Time{}, m.IssueErr
	}
	m.next++
	token := fmt.Sprintf("mock-token-%d", m.next)
	expiresAt := time.Now().Add(time.Hour)
	m.issued[token] = domain.Claims{UserName: user.Name, Role: user.Role, ExpiresAt: expiresAt}
	return token, expiresAt, nil
}

func (m *MockTokenIssuer) Verify(token string) (domain.Claims, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.VerifyErr != nil {
		return domain.Claims{}, m.VerifyErr
	}
	claims, ok := m.issued[token]
	if !ok {
		return domain.Claims{}, fmt.Errorf("invalid token")
	}
	if time.Now().After(claims.ExpiresAt) {
		return domain.Claims{}, fmt.Errorf("expired token")
	}
	return claims, nil
}
