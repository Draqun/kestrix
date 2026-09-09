import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import Login from './Login.jsx';

const auth = vi.hoisted(() => ({ login: vi.fn() }));
vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ login: auth.login }),
}));

test('submits the entered credentials', async () => {
  auth.login.mockResolvedValue({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  const user = userEvent.setup();
  render(<Login />);

  await user.type(screen.getByLabelText('Name'), 'alice');
  await user.type(screen.getByLabelText('Password'), 's3cret');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  assert.deepEqual(auth.login.mock.calls[0], ['alice', 's3cret']);
});

test('shows the server error on a rejected login', async () => {
  auth.login.mockRejectedValue(new Error('invalid name or password'));
  const user = userEvent.setup();
  render(<Login />);

  await user.type(screen.getByLabelText('Name'), 'alice');
  await user.type(screen.getByLabelText('Password'), 'wrong');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  assert.ok(await screen.findByText('invalid name or password'));
});

test('disables submit until both fields are filled', () => {
  render(<Login />);
  assert.equal(screen.getByRole('button', { name: 'Sign in' }).disabled, true);
});

test('shows the cookie hint when login answers 200 but the session cookie never arrives', async () => {
  // AuthContext.login signals this specific error code when the post-login
  // /me call 401s - see design.md D4 risk (Secure cookie over plain HTTP).
  // The exact wording is this page's own, not AuthContext's.
  const err = new Error('Signed in, but the session did not start.');
  err.code = 'SESSION_NOT_STARTED';
  auth.login.mockRejectedValue(err);
  const user = userEvent.setup();
  render(<Login />);

  await user.type(screen.getByLabelText('Name'), 'alice');
  await user.type(screen.getByLabelText('Password'), 's3cret');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  assert.ok(await screen.findByText(/KESTRIX_COOKIE_SECURE/));
});
