package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// loginThrottle is a fixed-window counter keyed independently by account
// name and by client address; see design.md D7. The account key stops a
// slow guess against one password; the address key stops a fast sweep
// across many account names from one client. Either key tripping refuses
// the attempt.
//
// POST /api/auth/login is public and unauthenticated, so an attacker can
// otherwise grow accounts without bound just by posting distinct account
// names, and addrs too when KESTRIX_TRUST_PROXY_HEADER trusts a forgeable
// X-Forwarded-For. maxEntries bounds both maps the same way
// InMemoryIdentityCache bounds itself: at capacity, inserting a genuinely
// new key evicts one arbitrary existing key first.
type loginThrottle struct {
	maxAttempts int
	maxEntries  int
	window      time.Duration
	now         func() time.Time

	mu       sync.Mutex
	accounts map[string]*throttleWindow
	addrs    map[string]*throttleWindow
}

type throttleWindow struct {
	failures   int
	windowEnds time.Time
}

// newLoginThrottle constructs a throttle allowing maxAttempts failures per
// window for each key, holding at most maxEntries distinct keys per map
// (accounts and addrs are bounded independently; maxEntries <= 0 means
// unbounded). now is the clock seam; production wiring passes time.Now,
// tests inject a controllable clock.
func newLoginThrottle(maxAttempts int, window time.Duration, maxEntries int, now func() time.Time) *loginThrottle {
	if now == nil {
		now = time.Now
	}
	return &loginThrottle{
		maxAttempts: maxAttempts,
		maxEntries:  maxEntries,
		window:      window,
		now:         now,
		accounts:    make(map[string]*throttleWindow),
		addrs:       make(map[string]*throttleWindow),
	}
}

// blocked reports whether key's current window in m has already reached
// maxAttempts failures, and if so, how long remains in that window. A
// window whose time has elapsed is treated as fresh without being deleted
// eagerly - recordFailure overwrites it on the next failure.
func (t *loginThrottle) blocked(m map[string]*throttleWindow, key string) (bool, time.Duration) {
	w, ok := m[key]
	if !ok {
		return false, 0
	}
	now := t.now()
	if !now.Before(w.windowEnds) {
		return false, 0
	}
	if w.failures >= t.maxAttempts {
		return true, w.windowEnds.Sub(now)
	}
	return false, 0
}

// Allow reports whether an attempt for account from addr may proceed. When
// it may not, retryAfter names how long the caller should wait before its
// blocking window rolls over. An empty account name is a valid key: an
// unknown account is throttled exactly like a known one.
func (t *loginThrottle) Allow(account, addr string) (ok bool, retryAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if blocked, wait := t.blocked(t.accounts, account); blocked {
		return false, wait
	}
	if blocked, wait := t.blocked(t.addrs, addr); blocked {
		return false, wait
	}
	return true, 0
}

// recordFailureLocked bumps key's failure count in m, starting a fresh
// window if key is absent or its window has elapsed. A genuinely new key
// evicts one arbitrary existing entry first when m is already at
// maxEntries - see loginThrottle's doc comment.
func (t *loginThrottle) recordFailureLocked(m map[string]*throttleWindow, key string) {
	now := t.now()
	if w, ok := m[key]; ok && now.Before(w.windowEnds) {
		w.failures++
		return
	}
	if _, ok := m[key]; !ok && t.maxEntries > 0 && len(m) >= t.maxEntries {
		for evict := range m {
			delete(m, evict)
			break
		}
	}
	m[key] = &throttleWindow{windowEnds: now.Add(t.window), failures: 1}
}

// RecordFailure counts one failed attempt against both the account and the
// address windows.
func (t *loginThrottle) RecordFailure(account, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordFailureLocked(t.accounts, account)
	t.recordFailureLocked(t.addrs, addr)
}

// RecordSuccess clears account's failure window. It does not touch any
// address window: other accounts may still be under attack from the same
// client, and a success on one account must not reset that.
func (t *loginThrottle) RecordSuccess(account string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accounts, account)
}

// AllowAddr and RecordFailureAddr are the address-only half of Allow and
// RecordFailure, used by throttleSetup: first-run setup has no account name
// to key by, only the client address; see first-run-setup design.md D4.
func (t *loginThrottle) AllowAddr(addr string) (ok bool, retryAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if blocked, wait := t.blocked(t.addrs, addr); blocked {
		return false, wait
	}
	return true, 0
}

func (t *loginThrottle) RecordFailureAddr(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordFailureLocked(t.addrs, addr)
}

// clientAddress resolves the caller's address for throttling. It trusts the
// first hop of X-Forwarded-For only when trustProxyHeader is set; reading it
// unconditionally would let any client forge its own throttle key. See
// design.md D7.
//
// r.RemoteAddr is "ip:port" - the port is the client's ephemeral source
// port, which a fresh TCP connection per attempt (the default for most HTTP
// clients, including a plain curl loop) changes on every request. Keying on
// the raw value would give an attacker a new throttle key per attempt,
// defeating the address key entirely, so the port is stripped before use.
func clientAddress(r *http.Request, trustProxyHeader bool) string {
	if trustProxyHeader {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if first := strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0]); first != "" {
				return first
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// loginNameProbe reads just enough of the login body to key the throttle -
// handleLogin still does its own, authoritative decode.
type loginNameProbe struct {
	Name string `json:"name"`
}

// statusRecorder captures the status code the wrapped handler writes, so
// throttleLogin can tell a successful login from a failed one without
// AuthService reporting back to it directly.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// throttleLogin wraps POST /api/auth/login. An account or client address
// that has already failed s.loginThrottle's limit within its window is
// rejected with 429 and a Retry-After header before the wrapped handler -
// and therefore before any password hashing - ever runs; see design.md D7.
// A nil s.loginThrottle disables throttling entirely and is a plain
// pass-through, used by tests unrelated to it.
func (s *Server) throttleLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.loginThrottle == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		var probe loginNameProbe
		_ = json.Unmarshal(body, &probe) // a body that fails to parse still gets an address-only check; handleLogin reports the real parse error

		addr := clientAddress(r, s.cfg.TrustProxyHeader)
		if ok, retryAfter := s.loginThrottle.Allow(probe.Name, addr); !ok {
			seconds := int(retryAfter.Seconds())
			if retryAfter > time.Duration(seconds)*time.Second {
				seconds++ // round up so Retry-After never undershoots
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			s.logAuthFailure(r, probe.Name, "login throttled")
			writeError(w, http.StatusTooManyRequests, "too many login attempts")
			return
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		if rec.status == http.StatusOK {
			s.loginThrottle.RecordSuccess(probe.Name)
		} else {
			s.loginThrottle.RecordFailure(probe.Name, addr)
		}
	})
}

// throttleSetup wraps POST /api/setup with the same limits as
// s.loginThrottle, keyed by client address only: setup has no account name
// to key by, and the endpoint closes itself after one success anyway; see
// first-run-setup design.md D4. It uses its own s.setupThrottle instance
// rather than s.loginThrottle's: setup answers a non-201 response forever
// once any account exists, and counting that permanent response against
// login's shared per-address budget would let an unauthenticated caller
// trip 429 on POST /api/auth/login for any other client behind the same
// address. A nil s.setupThrottle disables throttling entirely, same as
// throttleLogin.
func (s *Server) throttleSetup(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.setupThrottle == nil {
			next.ServeHTTP(w, r)
			return
		}
		addr := clientAddress(r, s.cfg.TrustProxyHeader)
		if ok, retryAfter := s.setupThrottle.AllowAddr(addr); !ok {
			seconds := int(retryAfter.Seconds())
			if retryAfter > time.Duration(seconds)*time.Second {
				seconds++ // round up so Retry-After never undershoots
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			s.logAuthFailure(r, "", "setup throttled")
			writeError(w, http.StatusTooManyRequests, "too many setup attempts")
			return
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		if rec.status != http.StatusCreated {
			s.setupThrottle.RecordFailureAddr(addr)
		}
	})
}
