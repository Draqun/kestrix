import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import Settings from './Settings.jsx';

vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ roleAtLeast: () => true }),
}));

let items;
let listCalls;

beforeEach(() => {
  items = [
    { ref: 'cred_gitlab', name: 'GitLab', type: 'token', provider: 'gitlab', host: 'gitlab.example', revoked: false, token: 'must-not-render' },
  ];
  listCalls = 0;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/credentials' && !options.method) {
      listCalls++;
      return new Response(JSON.stringify({ items: [...items] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/credentials' && options.method === 'POST') {
      const input = JSON.parse(options.body);
      const metadata = { ref: 'cred_new', name: input.name, type: input.type, provider: input.provider, host: input.host, revoked: false };
      items.push(metadata);
      return new Response(JSON.stringify(metadata), {
        status: 201, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/credentials/cred_gitlab/revoke' && options.method === 'POST') {
      items = items.map(item => item.ref === 'cred_gitlab' ? { ...item, revoked: true } : item);
      return new Response(null, { status: 204 });
    }
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({ items: [] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
  window.confirm = vi.fn(() => true);
});

test('Settings owns metadata-only credential lifecycle and refreshes mutations', async () => {
  const user = userEvent.setup();
  render(<Settings />);

  assert.ok(await screen.findByRole('heading', { name: 'Settings' }));
  assert.ok(screen.getByRole('heading', { name: 'Repository credentials' }));
  assert.ok(await screen.findByText('GitLab'));
  assert.equal(screen.queryByText('must-not-render'), null);

  await user.click(screen.getByRole('button', { name: 'Add credential' }));
  await user.type(screen.getByLabelText(/Display name/), 'Bitbucket');
  await user.type(screen.getByLabelText(/Token/), 'secret-token');
  await user.click(screen.getByRole('button', { name: 'Create credential' }));

  assert.ok(await screen.findByText('Bitbucket'));
  assert.equal(screen.queryByText('secret-token'), null);
  assert.equal(listCalls >= 2, true);

  const revoke = screen.getAllByRole('button', { name: 'Revoke', hidden: true })[0];
  await user.click(revoke);
  await vi.waitFor(() => assert.ok(screen.getByText('Revoked')));
  assert.equal(window.confirm.mock.calls.length, 1);
  assert.equal(listCalls >= 3, true);
});
