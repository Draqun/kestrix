import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { login as apiLogin, logout as apiLogout, me as apiMe, setUnauthorizedHandler } from '../api/client';

// ROLE_RANK mirrors the ordering internal/domain.Role enforces server-side
// (viewer < operator < admin). It exists here only to hide actions a role
// cannot use; the server is what actually enforces access.
const ROLE_RANK = { viewer: 1, operator: 2, admin: 3 };

/** roleAtLeast reports whether role grants at least the permissions of min. */
export function roleAtLeast(role, min) {
  return (ROLE_RANK[role] || 0) >= (ROLE_RANK[min] || 0);
}

const NO_SESSION = { name: null, role: null };

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
  const [claims, setClaims] = useState(NO_SESSION);
  // restoring covers the one GET /api/auth/me a mount fires to ask the
  // HttpOnly cookie whether a session already exists - there is nothing to
  // decode client-side any more. AppRoutes must not decide login-vs-app
  // before this resolves, or a signed-in reload would flash the login view.
  const [restoring, setRestoring] = useState(true);

  const clearSession = useCallback(() => setClaims(NO_SESSION), []);

  // The API client calls this once, from any request, on a 401 - session
  // expired or the account was deleted/demoted mid-session.
  useEffect(() => {
    setUnauthorizedHandler(clearSession);
  }, [clearSession]);

  useEffect(() => {
    apiMe()
      .then((session) => setClaims({ name: session.name, role: session.role }))
      // Only a 401 is evidence there is no session; any other failure (a
      // transient store outage, a network error) is not, and must not be
      // treated as an unauthenticated visitor - see RequireAuth's own
      // 401-vs-503 distinction on the server.
      .catch((err) => { if (err.status === 401) setClaims(NO_SESSION); })
      .finally(() => setRestoring(false));
  }, []);

  const login = useCallback(async (name, password) => {
    const resp = await apiLogin(name, password);
    // The login response already implies the name; ask /me for the
    // authoritative role rather than trusting a decoded claim. Only a 401
    // here is evidence the session never started (see the SESSION_NOT_STARTED
    // error below) - any other failure (a transient store outage, a network
    // error) is not, so fall back to the name-only claims already implied by
    // the 200 login rather than force a sign-out on it.
    try {
      const session = await apiMe();
      setClaims({ name: session.name, role: session.role });
    } catch (err) {
      if (err.status === 401) {
        clearSession();
        // Login answered 200 but the session cookie never came back - the
        // classic symptom of a Secure cookie on a plain-HTTP deployment;
        // see design.md D4 risk. The specific wording for this belongs to
        // the login page, not this data layer.
        const sessionErr = new Error('Signed in, but the session did not start.');
        sessionErr.code = 'SESSION_NOT_STARTED';
        throw sessionErr;
      }
      setClaims({ name, role: null });
    }
    return resp;
  }, [clearSession]);

  const logout = useCallback(async () => {
    await apiLogout();
    clearSession();
  }, [clearSession]);

  const value = {
    name: claims.name,
    role: claims.role,
    isAuthenticated: Boolean(claims.name),
    restoring,
    login,
    logout,
    roleAtLeast: (min) => roleAtLeast(claims.role, min),
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
