import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import AccountsSection from './AccountsSection.jsx';

vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({ roleAtLeast: () => true }),
}));

let listCalls;
let deletedUrl;

beforeEach(() => {
  listCalls = 0;
  deletedUrl = null;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      listCalls++;
      const items = listCalls === 1 ? [{ name: 'alice', role: 'viewer', grants: [] }] : [];
      return new Response(JSON.stringify({ items }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users/alice' && options.method === 'DELETE') {
      deletedUrl = url;
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
  vi.stubGlobal('confirm', () => true);
});

test('deleting an account removes the row once the request succeeds', async () => {
  const user = userEvent.setup();
  render(<AccountsSection />);

  assert.ok(await screen.findByText('alice'));
  await user.click(screen.getByRole('button', { name: 'Delete' }));

  await vi.waitFor(() => assert.equal(deletedUrl, '/api/users/alice'));
  await vi.waitFor(() => assert.equal(screen.queryByText('alice') === null, true));
});
