import assert from 'node:assert/strict';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import { AuthProvider, useAuth } from './AuthContext.jsx';

const api = vi.hoisted(() => ({ login: vi.fn(), logout: vi.fn(), me: vi.fn() }));
vi.mock('../api/client.js', () => ({
  login: (...args) => api.login(...args),
  logout: (...args) => api.logout(...args),
  me: (...args) => api.me(...args),
  setUnauthorizedHandler: (handler) => { globalThis.__unauthorizedHandler = handler; },
}));

function Probe() {
  const { isAuthenticated, restoring, name, role, login, logout } = useAuth();
  return (
    <div>
      <span data-testid="restoring">{restoring ? 'yes' : 'no'}</span>
      <span data-testid="state">{isAuthenticated ? 'in' : 'out'}</span>
      <span data-testid="name">{name || ''}</span>
      <span data-testid="role">{role || ''}</span>
      <button onClick={() => login('alice', 's3cret').catch((err) => {
        document.getElementById('login-error').textContent = err.code || err.message;
      })}>login</button>
      <button onClick={logout}>logout</button>
      <div id="login-error" data-testid="login-error" />
    </div>
  );
}

async function waitForRestoreToFinish() {
  await waitFor(() => assert.equal(screen.getByTestId('restoring').textContent, 'no'));
}

// httpError builds an Error carrying `.status`, matching what client.js's
// real me() attaches to a non-2xx response.
function httpError(status) {
  const err = new Error(`http ${status}`);
  err.status = status;
  return err;
}

beforeEach(() => {
  api.login.mockReset();
  api.logout.mockReset().mockResolvedValue(undefined);
  api.me.mockReset();
  delete globalThis.__unauthorizedHandler;
});

test('starts unauthenticated when /me reports no session on mount', async () => {
  api.me.mockRejectedValue(httpError(401));
  render(<AuthProvider><Probe /></AuthProvider>);

  await waitForRestoreToFinish();
  assert.equal(screen.getByTestId('state').textContent, 'out');
});

test('restores name and role from /me on mount (survives a reload)', async () => {
  api.me.mockResolvedValue({ name: 'bob', role: 'admin', expires_at: '2026-01-01T00:00:00Z' });
  render(<AuthProvider><Probe /></AuthProvider>);

  await waitForRestoreToFinish();
  assert.equal(screen.getByTestId('state').textContent, 'in');
  assert.equal(screen.getByTestId('name').textContent, 'bob');
  assert.equal(screen.getByTestId('role').textContent, 'admin');
});

test('login sets the name from the login response, then role from a follow-up me', async () => {
  api.me.mockRejectedValueOnce(httpError(401));
  const user = userEvent.setup();
  api.login.mockResolvedValue({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  api.me.mockResolvedValue({ name: 'alice', role: 'operator', expires_at: '2026-01-01T00:00:00Z' });

  render(<AuthProvider><Probe /></AuthProvider>);
  await waitForRestoreToFinish();

  await user.click(screen.getByText('login'));

  assert.equal(screen.getByTestId('state').textContent, 'in');
  assert.equal(screen.getByTestId('name').textContent, 'alice');
  assert.equal(screen.getByTestId('role').textContent, 'operator');
  assert.equal(api.me.mock.calls.length, 2); // the restore call, then login's follow-up
});

test('logout calls the API before clearing session state', async () => {
  api.me.mockResolvedValue({ name: 'carol', role: 'viewer', expires_at: '2026-01-01T00:00:00Z' });
  const user = userEvent.setup();

  render(<AuthProvider><Probe /></AuthProvider>);
  await waitForRestoreToFinish();
  assert.equal(screen.getByTestId('state').textContent, 'in');

  await user.click(screen.getByText('logout'));

  assert.equal(api.logout.mock.calls.length, 1);
  assert.equal(screen.getByTestId('state').textContent, 'out');
  assert.equal(screen.getByTestId('name').textContent, '');
});

test('a 401 reported by the API client logs the session out without calling logout', async () => {
  api.me.mockResolvedValue({ name: 'dana', role: 'viewer', expires_at: '2026-01-01T00:00:00Z' });
  render(<AuthProvider><Probe /></AuthProvider>);
  await waitForRestoreToFinish();
  assert.equal(screen.getByTestId('state').textContent, 'in');

  assert.equal(typeof globalThis.__unauthorizedHandler, 'function');
  globalThis.__unauthorizedHandler();

  await waitFor(() => assert.equal(screen.getByTestId('state').textContent, 'out'));
  assert.equal(api.logout.mock.calls.length, 0);
});

test('a 200 login followed by a 401 from me clears the session and signals SESSION_NOT_STARTED', async () => {
  api.me.mockRejectedValueOnce(httpError(401)); // initial restore
  const user = userEvent.setup();
  api.login.mockResolvedValue({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  api.me.mockRejectedValueOnce(httpError(401)); // login's follow-up

  render(<AuthProvider><Probe /></AuthProvider>);
  await waitForRestoreToFinish();

  await user.click(screen.getByText('login'));

  assert.equal(screen.getByTestId('state').textContent, 'out');
  assert.equal(screen.getByTestId('login-error').textContent, 'SESSION_NOT_STARTED');
});

test('a 200 login followed by a non-401 me failure stays signed in with the known name', async () => {
  api.me.mockRejectedValueOnce(httpError(401)); // initial restore
  const user = userEvent.setup();
  api.login.mockResolvedValue({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  api.me.mockRejectedValueOnce(httpError(503)); // login's follow-up: a transient outage

  render(<AuthProvider><Probe /></AuthProvider>);
  await waitForRestoreToFinish();

  await user.click(screen.getByText('login'));

  // The login itself succeeded and the cookie is presumably set; a
  // transient failure of the follow-up /me call must not be treated as
  // evidence the session never started - unlike the 401 case above.
  assert.equal(screen.getByTestId('state').textContent, 'in');
  assert.equal(screen.getByTestId('name').textContent, 'alice');
  assert.equal(screen.getByTestId('role').textContent, '');
  assert.equal(screen.getByTestId('login-error').textContent, '');
});

test('a rejected login leaves the session unauthenticated', async () => {
  api.me.mockRejectedValue(httpError(401));
  const user = userEvent.setup();
  api.login.mockRejectedValue(new Error('invalid name or password'));

  render(<AuthProvider><Probe /></AuthProvider>);
  await waitForRestoreToFinish();
  await user.click(screen.getByText('login'));

  assert.equal(screen.getByTestId('state').textContent, 'out');
});
