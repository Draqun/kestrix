import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { beforeEach, test, vi } from 'vitest';
import AccountsSection from './AccountsSection.jsx';

const authState = vi.hoisted(() => ({ role: 'admin' }));
vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({
    roleAtLeast: (min) => {
      const rank = { viewer: 1, operator: 2, admin: 3 };
      return (rank[authState.role] || 0) >= (rank[min] || 0);
    },
  }),
}));

beforeEach(() => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({
        items: [{ name: 'alice', role: 'admin', grants: [{ project_name: 'demo', role: 'operator' }] }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    throw new Error(`unexpected request: ${url}`);
  });
});

test('a non-admin session renders nothing and never requests the account list', async () => {
  authState.role = 'viewer';
  const { container } = render(<AccountsSection />);
  assert.equal(container.firstChild, null);

  // Give any stray effect a tick to fire before asserting silence.
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(globalThis.fetch.mock.calls.length, 0);
});

test('an admin session renders the account list', async () => {
  authState.role = 'admin';
  render(<AccountsSection />);

  assert.ok(await screen.findByText('alice'));
  assert.ok(screen.getByText('demo: operator'));
});

test('a password value or hash in the API response never renders on the page', async () => {
  authState.role = 'admin';
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({
        items: [{
          name: 'alice',
          role: 'admin',
          grants: [],
          // Defensive: even if a future backend response started including
          // these, the spec requires they never appear on the page.
          password: 'must-not-render',
          password_hash: '$argon2id$must-not-render',
        }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    throw new Error(`unexpected request: ${url}`);
  });

  render(<AccountsSection />);

  assert.ok(await screen.findByText('alice'));
  assert.equal(screen.queryByText('must-not-render'), null);
  assert.equal(screen.queryByText(/\$argon2id\$/), null);
  assert.equal(document.body.innerHTML.includes('must-not-render'), false);
});
