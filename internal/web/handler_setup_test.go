package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kestrix/internal/domain"
)

func TestHandleSetupStatus_RequiredOnEmptyInstance(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/setup", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp setupStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Required {
		t.Fatal("Required = false on an empty instance, want true")
	}
}

func TestHandleSetupStatus_NotRequiredOnceAnAccountExists(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "alice", "s3cret-pass", domain.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/setup", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp setupStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Required {
		t.Fatal("Required = true once an account exists, want false")
	}
}

func TestHandleSetupStatus_StoreError(t *testing.T) {
	store := newErrUserStore()
	store.HasAnyErr = errors.New("boom")
	srv := &Server{userStore: store}
	w := httptest.NewRecorder()
	srv.handleSetupStatus(w, httptest.NewRequest(http.MethodGet, "/api/setup", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetupStatus_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleSetupStatus(w, httptest.NewRequest(http.MethodGet, "/api/setup", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleSetup_CreatesAdminOnEmptyInstance(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(setupRequest{Name: "admin", Password: "s3cret-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", w.Code, w.Body.String())
	}
	var view userView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if view.Name != "admin" || view.Role != domain.RoleAdmin {
		t.Fatalf("created = %+v, want name=admin role=admin", view)
	}

	// The new account can immediately log in.
	loginBody, _ := json.Marshal(loginRequest{Name: "admin", Password: "s3cret-pass"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	srv.router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login after setup status = %d, want 200 (body=%s)", loginW.Code, loginW.Body.String())
	}
}

func TestHandleSetup_ConflictOnceAnAccountExists(t *testing.T) {
	srv := NewServer(Config{})
	createTestUser(t, srv, "existing", "s3cret-pass", domain.RoleViewer)

	body, _ := json.Marshal(setupRequest{Name: "admin", Password: "s3cret-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if _, err := srv.userStore.GetUser("admin"); err == nil {
		t.Fatal("setup created an account after the instance already had one")
	}
}

func TestHandleSetup_MissingFields(t *testing.T) {
	srv := NewServer(Config{})
	body, _ := json.Marshal(setupRequest{Name: "", Password: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSetup_InvalidJSON(t *testing.T) {
	srv := NewServer(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSetup_HashFails(t *testing.T) {
	srv := &Server{userStore: newErrUserStore(), passwordHasher: &fakeHasher{hashErr: errors.New("boom")}}
	w := httptest.NewRecorder()
	body := jsonBody(setupRequest{Name: "admin", Password: "p"})
	srv.handleCompleteSetup(w, httptest.NewRequest(http.MethodPost, "/api/setup", body))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetup_StoreErrorOtherThanSetupDone(t *testing.T) {
	store := newErrUserStore()
	store.CreateFirstErr = errors.New("boom")
	srv := &Server{userStore: store, passwordHasher: &fakeHasher{}}
	w := httptest.NewRecorder()
	body := jsonBody(setupRequest{Name: "admin", Password: "p"})
	srv.handleCompleteSetup(w, httptest.NewRequest(http.MethodPost, "/api/setup", body))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleSetup_NilStore(t *testing.T) {
	srv := &Server{}
	w := httptest.NewRecorder()
	srv.handleCompleteSetup(w, httptest.NewRequest(http.MethodPost, "/api/setup", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleSetup_NilPasswordHasher(t *testing.T) {
	srv := &Server{userStore: newErrUserStore()}
	w := httptest.NewRecorder()
	body := jsonBody(setupRequest{Name: "admin", Password: "p"})
	srv.handleCompleteSetup(w, httptest.NewRequest(http.MethodPost, "/api/setup", body))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestThrottleSetup_BlocksAfterLimitWithRetryAfter(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 2, time.Minute, clock)

	body, _ := json.Marshal(setupRequest{Name: "", Password: ""}) // invalid: keeps every attempt a non-201 failure
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: status = %d, want 400", i, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing on a 429 response")
	}
}

func TestThrottleSetup_RetryAfterRoundsUpFractionalSeconds(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, 1500*time.Millisecond, clock)
	body, _ := json.Marshal(setupRequest{Name: "", Password: ""})

	req1 := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	srv.router.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w2.Code)
	}
	if got := w2.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2 (1.5s window rounded up)", got)
	}
}

func TestThrottleSetup_KeyedByAddressNotAccount(t *testing.T) {
	// Setup has no account name; two different addresses must not share a
	// throttle window - see first-run-setup design.md D4.
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock)

	body, _ := json.Marshal(setupRequest{Name: "", Password: ""})
	req1 := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	req1.RemoteAddr = "1.1.1.1:1111"
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("first address attempt status = %d, want 400", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body))
	req2.RemoteAddr = "2.2.2.2:2222"
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("second address attempt status = %d, want 400 (must not be throttled by the first address)", w2.Code)
	}
}

// TestThrottleSetup_DoesNotShareBucketWithLogin pins R1 from REVIEW.md:
// setup answers a permanent non-201 (409) forever once an account exists,
// so if it shared loginThrottle's per-address counter, an unauthenticated
// caller could exhaust that budget by POSTing to /api/setup and trip 429 on
// /api/auth/login for any other client behind the same address.
func TestThrottleSetup_DoesNotShareBucketWithLogin(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock)
	createTestUser(t, srv, "alice", "correct-password", domain.RoleViewer)
	const addr = "9.9.9.9:1234"

	// Exhaust the setup throttle for this address (maxAttempts=1): the
	// account already exists, so this first attempt answers 409 and counts
	// as one throttle failure.
	setupBody, _ := json.Marshal(setupRequest{Name: "admin", Password: "whatever"})
	seedReq := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(setupBody))
	seedReq.RemoteAddr = addr
	seedW := httptest.NewRecorder()
	srv.router.ServeHTTP(seedW, seedReq)
	if seedW.Code != http.StatusConflict {
		t.Fatalf("seed setup attempt status = %d, want 409", seedW.Code)
	}

	setupProbe := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(setupBody))
	setupProbe.RemoteAddr = addr
	setupW := httptest.NewRecorder()
	srv.router.ServeHTTP(setupW, setupProbe)
	if setupW.Code != http.StatusTooManyRequests {
		t.Fatalf("setup status = %d, want 429 (setup's own budget must now be exhausted)", setupW.Code)
	}

	// A login from the very same address must be unaffected.
	loginBody, _ := json.Marshal(loginRequest{Name: "alice", Password: "correct-password"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.RemoteAddr = addr
	loginW := httptest.NewRecorder()
	srv.router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (must not be throttled by setup's exhausted budget, body=%s)", loginW.Code, loginW.Body.String())
	}
}
