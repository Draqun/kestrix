import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import { beforeEach, test, vi } from 'vitest';
import Settings from './Settings.jsx';

vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ roleAtLeast: () => true }),
}));

beforeEach(() => {
  window.innerWidth = 375;
  window.matchMedia = vi.fn().mockImplementation(query => ({
    matches: query.includes('max-width:599.95px'),
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent() { return false; },
  }));
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({ items: [] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    return new Response(JSON.stringify({
      items: [{ ref: 'cred_mobile', name: 'Mobile Git', type: 'token', revoked: false }],
    }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
});

test('Settings keeps credential metadata and lifecycle actions usable at 375px', async () => {
  render(<Settings />);

  assert.ok(await screen.findByRole('heading', { name: 'Repository credentials' }));
  assert.ok(await screen.findByText('Mobile Git'));
  const actions = screen.getByTestId('credential-actions-cred_mobile');
  assert.ok(within(actions).getByRole('button', { name: 'Rotate' }));
  assert.ok(within(actions).getByRole('button', { name: 'Revoke' }));
  assert.ok(screen.getByRole('button', { name: 'Add credential' }));
  assert.equal(document.documentElement.scrollWidth <= 375, true);
});
