import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { test, vi } from 'vitest';
import App from './App.jsx';

const auth = vi.hoisted(() => ({ isAuthenticated: false }));
vi.mock('./context/AuthContext.jsx', () => ({
  AuthProvider: ({ children }) => children,
  useAuth: () => ({
    isAuthenticated: auth.isAuthenticated,
    name: 'test-user',
    role: 'viewer',
    login: vi.fn(),
    logout: vi.fn(),
    roleAtLeast: () => true,
  }),
}));
vi.mock('./hooks/useKeyboardNav.js', () => ({ default: () => {} }));

test('an unauthenticated visitor sees the login view, not application data', () => {
  auth.isAuthenticated = false;
  // No fetch mock at all: if any project/run/dashboard data were fetched,
  // this call would throw and fail the test.
  globalThis.fetch = vi.fn(() => { throw new Error('must not call the API while unauthenticated'); });

  render(<App />);

  assert.ok(screen.getByRole('heading', { name: 'Sign in to Kestrix' }));
  assert.equal(screen.queryByText('Dashboard'), null);
  assert.equal(globalThis.fetch.mock.calls.length, 0);
});

test('an authenticated visitor sees the application, not the login view', async () => {
  auth.isAuthenticated = true;
  globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({}), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  }));

  render(<App />);

  assert.equal(screen.queryByRole('heading', { name: 'Sign in to Kestrix' }), null);
  assert.ok(await screen.findByText('Kestrix'));
});
