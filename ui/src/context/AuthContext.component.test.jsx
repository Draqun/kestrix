import assert from 'node:assert/strict';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import { AuthProvider, useAuth } from './AuthContext.jsx';

const apiLogin = vi.hoisted(() => vi.fn());
vi.mock('../api/client.js', () => ({
  login: (...args) => apiLogin(...args),
  setAuthToken: () => {},
  setUnauthorizedHandler: (handler) => { globalThis.__unauthorizedHandler = handler; },
}));

function makeToken(claims) {
  const header = btoa(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const payload = btoa(JSON.stringify(claims));
  return `${header}.${payload}.fake-signature`;
}

function Probe() {
  const { isAuthenticated, name, role, login, logout } = useAuth();
  return (
    <div>
      <span data-testid="state">{isAuthenticated ? 'in' : 'out'}</span>
      <span data-testid="name">{name || ''}</span>
      <span data-testid="role">{role || ''}</span>
      <button onClick={() => login('alice', 's3cret').catch(() => {})}>login</button>
      <button onClick={logout}>logout</button>
    </div>
  );
}

beforeEach(() => {
  apiLogin.mockReset();
  try { sessionStorage.clear(); } catch {}
  delete globalThis.__unauthorizedHandler;
});

test('starts unauthenticated with no stored token', () => {
  render(<AuthProvider><Probe /></AuthProvider>);
  assert.equal(screen.getByTestId('state').textContent, 'out');
});

test('login stores the token and decodes its identity', async () => {
  const user = userEvent.setup();
  apiLogin.mockResolvedValue({ token: makeToken({ sub: 'alice', role: 'operator' }), expires_at: '2026-01-01T00:00:00Z' });

  render(<AuthProvider><Probe /></AuthProvider>);
  await user.click(screen.getByText('login'));

  assert.equal(screen.getByTestId('state').textContent, 'in');
  assert.equal(screen.getByTestId('name').textContent, 'alice');
  assert.equal(screen.getByTestId('role').textContent, 'operator');
  assert.equal(sessionStorage.getItem('kestrix-auth-token'), makeToken({ sub: 'alice', role: 'operator' }));
});

test('restores a token already in sessionStorage on mount (survives a reload)', () => {
  const token = makeToken({ sub: 'bob', role: 'admin' });
  sessionStorage.setItem('kestrix-auth-token', token);

  render(<AuthProvider><Probe /></AuthProvider>);

  assert.equal(screen.getByTestId('state').textContent, 'in');
  assert.equal(screen.getByTestId('name').textContent, 'bob');
  assert.equal(screen.getByTestId('role').textContent, 'admin');
});

test('logout drops the token from state and sessionStorage', async () => {
  const user = userEvent.setup();
  sessionStorage.setItem('kestrix-auth-token', makeToken({ sub: 'carol', role: 'viewer' }));

  render(<AuthProvider><Probe /></AuthProvider>);
  assert.equal(screen.getByTestId('state').textContent, 'in');

  await user.click(screen.getByText('logout'));

  assert.equal(screen.getByTestId('state').textContent, 'out');
  assert.equal(screen.getByTestId('name').textContent, '');
  assert.equal(sessionStorage.getItem('kestrix-auth-token'), null);
});

test('a 401 reported by the API client logs the session out', () => {
  sessionStorage.setItem('kestrix-auth-token', makeToken({ sub: 'dana', role: 'viewer' }));
  render(<AuthProvider><Probe /></AuthProvider>);
  assert.equal(screen.getByTestId('state').textContent, 'in');

  assert.equal(typeof globalThis.__unauthorizedHandler, 'function');
  act(() => { globalThis.__unauthorizedHandler(); });

  assert.equal(screen.getByTestId('state').textContent, 'out');
});

test('a rejected login leaves the session unauthenticated', async () => {
  const user = userEvent.setup();
  apiLogin.mockRejectedValue(new Error('invalid name or password'));

  render(<AuthProvider><Probe /></AuthProvider>);
  await user.click(screen.getByText('login'));

  assert.equal(screen.getByTestId('state').textContent, 'out');
});
