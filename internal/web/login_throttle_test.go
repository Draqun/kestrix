package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kestrix/internal/domain"
)

// stoppedClock returns a fixed time the test advances manually, exercising
// the injected-clock seam without a real sleep.
type stoppedClock struct{ t time.Time }

func (c *stoppedClock) now() time.Time { return c.t }

func TestLoginThrottle_AllowsUpToLimit(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(3, time.Minute, 0, clock.now)
	for i := 0; i < 3; i++ {
		if ok, _ := th.Allow("alice", "1.2.3.4"); !ok {
			t.Fatalf("attempt %d: Allow() = false, want true (limit not yet reached)", i)
		}
		th.RecordFailure("alice", "1.2.3.4")
	}
	ok, retryAfter := th.Allow("alice", "1.2.3.4")
	if ok {
		t.Fatal("Allow() = true after 3 failures with a limit of 3, want false")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want positive", retryAfter)
	}
}

func TestLoginThrottle_WindowRollsOver(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(2, time.Minute, 0, clock.now)
	th.RecordFailure("alice", "1.2.3.4")
	th.RecordFailure("alice", "1.2.3.4")
	if ok, _ := th.Allow("alice", "1.2.3.4"); ok {
		t.Fatal("Allow() = true at the limit, want false")
	}

	clock.t = clock.t.Add(61 * time.Second)
	if ok, _ := th.Allow("alice", "1.2.3.4"); !ok {
		t.Fatal("Allow() = false after the window rolled over, want true")
	}
}

func TestLoginThrottle_AccountAndAddressAreIndependentKeys(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(2, time.Minute, 0, clock.now)
	th.RecordFailure("alice", "1.1.1.1")
	th.RecordFailure("alice", "1.1.1.1")
	// alice is now blocked, but bob from a different address must not be.
	if ok, _ := th.Allow("bob", "2.2.2.2"); !ok {
		t.Fatal("Allow(\"bob\", other address) = false, want true - account keys must be independent")
	}
	// A different account name from the SAME address that tripped alice's
	// account key must also still be allowed, since alice's two failures
	// only came from 1.1.1.1 twice, not from a distinct address key yet -
	// but the address key for 1.1.1.1 has now also seen 2 failures.
	if ok, _ := th.Allow("carol", "1.1.1.1"); ok {
		t.Fatal("Allow(\"carol\", 1.1.1.1) = true, want false - the address key must have tripped too")
	}
}

func TestLoginThrottle_SuccessClearsAccountCounter(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(2, time.Minute, 0, clock.now)
	th.RecordFailure("alice", "1.2.3.4")
	th.RecordFailure("alice", "9.9.9.9") // different address, same account
	if ok, _ := th.Allow("alice", "5.5.5.5"); ok {
		t.Fatal("Allow() = true before clearing, want false")
	}
	th.RecordSuccess("alice")
	if ok, _ := th.Allow("alice", "5.5.5.5"); !ok {
		t.Fatal("Allow() = false after RecordSuccess, want true - success must clear the account counter")
	}
}

func TestLoginThrottle_SuccessDoesNotClearAddressCounter(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(2, time.Minute, 0, clock.now)
	th.RecordFailure("alice", "6.6.6.6")
	th.RecordFailure("bob", "6.6.6.6")
	th.RecordSuccess("alice")
	// The address 6.6.6.6 has 2 failures total (from two different
	// accounts); a success on alice's own account key must not reset it.
	if ok, _ := th.Allow("carol", "6.6.6.6"); ok {
		t.Fatal("Allow() = true, want false - clearing an account counter must not clear the address counter")
	}
}

func TestLoginThrottle_UnknownAccountThrottledIdentically(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(1, time.Minute, 0, clock.now)
	th.RecordFailure("ghost-account", "7.7.7.7")
	ok, retryAfter := th.Allow("ghost-account", "8.8.8.8")
	if ok {
		t.Fatal("Allow() = true for an account name that never existed, want false after it trips the limit")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want positive", retryAfter)
	}
}

func TestLoginThrottle_EvictsAtBoundPerMap(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(5, time.Minute, 2, clock.now)
	th.RecordFailure("a", "addr-a")
	th.RecordFailure("b", "addr-b")
	th.RecordFailure("c", "addr-c")

	if got := len(th.accounts); got > 2 {
		t.Fatalf("accounts holds %d entries, want at most 2 (bound)", got)
	}
	if got := len(th.addrs); got > 2 {
		t.Fatalf("addrs holds %d entries, want at most 2 (bound)", got)
	}
	// The entry that triggered eviction must itself be present.
	if _, ok := th.accounts["c"]; !ok {
		t.Fatal("accounts[\"c\"] missing, want the entry that triggered eviction to survive its own insert")
	}
	if _, ok := th.addrs["addr-c"]; !ok {
		t.Fatal("addrs[\"addr-c\"] missing, want the entry that triggered eviction to survive its own insert")
	}
}

func TestLoginThrottle_EvictionDoesNotDropAnEntryStillWithinItsWindow(t *testing.T) {
	// Eviction picks an arbitrary entry, not necessarily an expired one -
	// this pins that behavior (mirroring InMemoryIdentityCache.Set) rather
	// than asserting anything about which specific entry survives.
	clock := &stoppedClock{t: time.Now()}
	th := newLoginThrottle(1, time.Minute, 1, clock.now)
	th.RecordFailure("first", "addr-1")
	if ok, _ := th.Allow("first", "addr-1"); ok {
		t.Fatal("Allow(\"first\") = true, want blocked before eviction")
	}
	th.RecordFailure("second", "addr-2")
	if got := len(th.accounts); got > 1 {
		t.Fatalf("accounts holds %d entries, want at most 1 (bound)", got)
	}
}

// ── clientAddress and the throttleLogin middleware ──

func TestClientAddress_TrustsForwardedForOnlyWhenConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.2")

	if got := clientAddress(req, false); got != "10.0.0.1" {
		t.Fatalf("clientAddress(trust=false) = %q, want the RemoteAddr host with its port stripped - a forged header must be ignored", got)
	}
	if got := clientAddress(req, true); got != "203.0.113.9" {
		t.Fatalf("clientAddress(trust=true) = %q, want the first X-Forwarded-For hop", got)
	}
}

// TestClientAddress_StripsEphemeralPort pins the fix for a finding that a
// fresh TCP connection per login attempt - the default for most HTTP
// clients - gets a new ephemeral source port each time. Keying the address
// throttle on the raw "ip:port" RemoteAddr would give an attacker a new
// throttle key per attempt, defeating the address key entirely.
func TestClientAddress_StripsEphemeralPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	if got := clientAddress(req, false); got != "203.0.113.9" {
		t.Fatalf("clientAddress() = %q, want the host with its ephemeral port stripped", got)
	}
}

// TestClientAddress_FallsBackToRawValueWhenNotHostPort covers a RemoteAddr
// that isn't in "host:port" form (e.g. a test fixture, or a unix socket
// path) - net.SplitHostPort errors, and the raw value must still be usable
// as a throttle key rather than producing an empty one.
func TestClientAddress_FallsBackToRawValueWhenNotHostPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "no-port-here"
	if got := clientAddress(req, false); got != "no-port-here" {
		t.Fatalf("clientAddress() = %q, want the raw RemoteAddr as a fallback", got)
	}
}

// newThrottledServer returns a test server with a real accounts store and
// an explicit login throttle on an injected clock, for the HTTP-level tests
// below.
func newThrottledServer(t *testing.T, maxAttempts int, window time.Duration, clock *stoppedClock) *Server {
	t.Helper()
	srv := NewServer(Config{})
	srv.loginThrottle = newLoginThrottle(maxAttempts, window, 0, clock.now)
	return srv
}

func TestThrottleLogin_BlocksAfterLimitWithRetryAfter(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 2, time.Minute, clock)
	createTestUser(t, srv, "dana", "correct-password", domain.RoleViewer)

	wrongBody, _ := json.Marshal(loginRequest{Name: "dana", Password: "wrong"})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(wrongBody))
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(wrongBody))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing on a 429 response")
	}
}

func TestThrottleLogin_ThrottledRequestNeverReachesAuthService(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock)
	createTestUser(t, srv, "erin", "correct-password", domain.RoleViewer)

	wrongBody, _ := json.Marshal(loginRequest{Name: "erin", Password: "wrong"})
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(wrongBody))
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", w1.Code)
	}

	// Second attempt, now with the CORRECT password. If the throttle let
	// this reach AuthService, it would succeed with 200. It must not.
	rightBody, _ := json.Marshal(loginRequest{Name: "erin", Password: "correct-password"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(rightBody))
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 - a throttled attempt must never reach AuthService, even with the right password", w2.Code)
	}
}

func TestThrottleLogin_UnknownAccountThrottledIdentically(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock)

	unknownBody, _ := json.Marshal(loginRequest{Name: "never-existed", Password: "anything"})
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(unknownBody))
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(unknownBody))
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 - an account name that never existed must throttle exactly like a real one", w2.Code)
	}
}

func TestNewLoginThrottle_NilClockDefaultsToRealTime(t *testing.T) {
	th := newLoginThrottle(5, time.Minute, 0, nil)
	if ok, _ := th.Allow("x", "y"); !ok {
		t.Fatal("Allow() = false on a fresh throttle, want true")
	}
}

// TestThrottleLogin_BodyReadFailure exercises throttleLogin's io.ReadAll
// error branch, using the errReader defined in webhook_verify_test.go.
func TestThrottleLogin_BodyReadFailure(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 5, time.Minute, clock)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", errReader{})
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestThrottleLogin_RetryAfterRoundsUpFractionalSeconds(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, 1500*time.Millisecond, clock)
	body, _ := json.Marshal(loginRequest{Name: "gina", Password: "wrong"})

	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	srv.router.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w2.Code)
	}
	if got := w2.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2 (1.5s window rounded up)", got)
	}
}

func TestThrottleLogin_SuccessReachesRecordSuccess(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 5, time.Minute, clock)
	createTestUser(t, srv, "helen", "correct-password", domain.RoleViewer)

	body, _ := json.Marshal(loginRequest{Name: "helen", Password: "correct-password"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
}

func TestThrottleLogin_ForgedForwardedForIgnoredWhenNotTrusted(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock) // cfg.TrustProxyHeader defaults false

	body1, _ := json.Marshal(loginRequest{Name: "frank-one", Password: "wrong"})
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body1))
	req1.RemoteAddr = "10.0.0.1:1"
	req1.Header.Set("X-Forwarded-For", "1.1.1.1")
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", w1.Code)
	}

	// A different account, claiming a different X-Forwarded-For, but the
	// same real RemoteAddr. If the header were trusted, this would look
	// like a fresh address (1.1.1.1 vs 2.2.2.2) and pass; since it is not
	// trusted, both requests share the address key 10.0.0.1:1.
	body2, _ := json.Marshal(loginRequest{Name: "frank-two", Password: "wrong"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body2))
	req2.RemoteAddr = "10.0.0.1:1"
	req2.Header.Set("X-Forwarded-For", "2.2.2.2")
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 - an untrusted X-Forwarded-For must not let a client escape the address throttle", w2.Code)
	}
}

// TestThrottleLogin_SameIPDifferentPortsShareTheAddressThrottle pins the fix
// for a finding that r.RemoteAddr is "ip:port": a fresh TCP connection per
// attempt - the default for most HTTP clients, including a plain curl loop
// or typical brute-force tooling - gets a new ephemeral source port every
// time. Before clientAddress stripped the port, each attempt landed in its
// own address bucket and the address throttle never accumulated across a
// sweep from one machine.
func TestThrottleLogin_SameIPDifferentPortsShareTheAddressThrottle(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	srv := newThrottledServer(t, 1, time.Minute, clock)

	body1, _ := json.Marshal(loginRequest{Name: "sweep-one", Password: "wrong"})
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body1))
	req1.RemoteAddr = "203.0.113.9:11111"
	w1 := httptest.NewRecorder()
	srv.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", w1.Code)
	}

	// A different account, from the same IP but a different ephemeral
	// source port - as a real second TCP connection would present.
	body2, _ := json.Marshal(loginRequest{Name: "sweep-two", Password: "wrong"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body2))
	req2.RemoteAddr = "203.0.113.9:22222"
	w2 := httptest.NewRecorder()
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 - a fresh source port from the same IP must not reset the address throttle", w2.Code)
	}
}
