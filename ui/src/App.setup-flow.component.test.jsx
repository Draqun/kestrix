import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import App from './App.jsx';

// Unlike App.component.test.jsx (which mocks AuthContext outright to test
// routing in isolation), this file drives the real AuthProvider so it can
// pin REVIEW.md R3: a successful setup submission must actually reach a
// post-setup, signed-in view - not just call completeSetup with the right
// arguments. Only the API boundary is mocked.
const api = vi.hoisted(() => ({
  setupStatus: vi.fn(), completeSetup: vi.fn(), login: vi.fn(), me: vi.fn(),
  logout: vi.fn(), getDashboard: vi.fn(), getVersion: vi.fn(),
}));
vi.mock('./api/client.js', () => ({
  setupStatus: (...args) => api.setupStatus(...args),
  completeSetup: (...args) => api.completeSetup(...args),
  login: (...args) => api.login(...args),
  me: (...args) => api.me(...args),
  logout: (...args) => api.logout(...args),
  getDashboard: (...args) => api.getDashboard(...args),
  getVersion: (...args) => api.getVersion(...args),
  setUnauthorizedHandler: () => {},
}));
vi.mock('./hooks/useKeyboardNav.js', () => ({ default: () => {} }));

test('completing setup signs in and lands on the real dashboard view', async () => {
  api.setupStatus.mockResolvedValue({ required: true });
  api.me.mockRejectedValueOnce(Object.assign(new Error('no session'), { status: 401 })); // initial restore
  const user = userEvent.setup();

  render(<App />);

  assert.ok(await screen.findByRole('heading', { name: 'Create the first administrator' }));

  api.completeSetup.mockResolvedValue({ name: 'admin', role: 'admin' });
  api.login.mockResolvedValue({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  api.me.mockResolvedValue({ name: 'admin', role: 'admin', expires_at: '2026-01-01T00:00:00Z' });
  api.getDashboard.mockResolvedValue({ projects: 0, total_runs: 0 });
  api.getVersion.mockResolvedValue({ version: 'test' });

  await user.type(screen.getByLabelText('Name'), 'admin');
  await user.type(screen.getByLabelText('Password'), 's3cret-pass');
  await user.type(screen.getByLabelText('Confirm password'), 's3cret-pass');
  await user.click(screen.getByRole('button', { name: 'Create administrator' }));

  // findByRole('heading', ...) is unreliable here: MUI's Drawer renders as a
  // "temporary" (modal) variant under jsdom's default matchMedia result and
  // marks the rest of the tree aria-hidden while open, which the
  // accessibility-tree-aware role query would then miss entirely - the same
  // reason the sibling "authenticated visitor" test in App.component.test.jsx
  // asserts via findByText rather than findByRole. "Dashboard" also matches
  // the nav link, so assert on page body content that is unique to the
  // Dashboard view.
  assert.ok(await screen.findByText('Recent Runs'));
  assert.equal(screen.queryByText('Create the first administrator'), null);
  assert.equal(screen.queryByText('Sign in to Kestrix'), null);
});
