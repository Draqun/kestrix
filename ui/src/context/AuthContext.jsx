import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { login as apiLogin, setAuthToken, setUnauthorizedHandler } from '../api/client';

const STORAGE_KEY = 'kestrix-auth-token';

// ROLE_RANK mirrors the ordering internal/domain.Role enforces server-side
// (viewer < operator < admin). It exists here only to hide actions a role
// cannot use; the server is what actually enforces access.
const ROLE_RANK = { viewer: 1, operator: 2, admin: 3 };

/** roleAtLeast reports whether role grants at least the permissions of min. */
export function roleAtLeast(role, min) {
  return (ROLE_RANK[role] || 0) >= (ROLE_RANK[min] || 0);
}

function readStoredToken() {
  try {
    return sessionStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

function storeToken(token) {
  try {
    if (token) sessionStorage.setItem(STORAGE_KEY, token);
    else sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // sessionStorage unavailable (private mode, etc.) - the session simply
    // does not survive a reload, it still works for the current page life.
  }
}

// decodeClaims reads the name and role a token carries, without verifying
// its signature: the UI only uses this to decide what to show, and every
// request the token authorizes is re-checked by the server regardless.
function decodeClaims(token) {
  try {
    const payload = token.split('.')[1];
    const base64 = payload.replace(/-/g, '+').replace(/_/g, '/');
    const json = JSON.parse(atob(base64));
    return { name: json.sub || null, role: json.role || null };
  } catch {
    return { name: null, role: null };
  }
}

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
  const [token, setTokenState] = useState(() => readStoredToken());
  const [claims, setClaims] = useState(() => {
    const stored = readStoredToken();
    return stored ? decodeClaims(stored) : { name: null, role: null };
  });

  useEffect(() => {
    setAuthToken(token);
  }, [token]);

  const logout = useCallback(() => {
    setTokenState(null);
    setClaims({ name: null, role: null });
    storeToken(null);
  }, []);

  // The API client calls this once, from any request, on a 401 - session
  // expired or the account was deleted/demoted mid-session.
  useEffect(() => {
    setUnauthorizedHandler(logout);
  }, [logout]);

  const login = useCallback(async (name, password) => {
    const resp = await apiLogin(name, password);
    setTokenState(resp.token);
    setClaims(decodeClaims(resp.token));
    storeToken(resp.token);
    return resp;
  }, []);

  const value = {
    token,
    name: claims.name,
    role: claims.role,
    isAuthenticated: Boolean(token),
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
