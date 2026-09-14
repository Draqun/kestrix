import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import AccountsSection from './AccountsSection.jsx';

vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({ roleAtLeast: () => true }),
}));

let listCalls;
let createPayload;
let nextCreateStatus;

beforeEach(() => {
  listCalls = 0;
  createPayload = null;
  nextCreateStatus = 201;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      listCalls++;
      const items = listCalls === 1 ? [] : [{ name: 'bob', role: 'viewer', grants: [] }];
      return new Response(JSON.stringify({ items }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users' && options.method === 'POST') {
      createPayload = JSON.parse(options.body);
      if (nextCreateStatus !== 201) {
        return new Response(JSON.stringify({ error: 'user already exists' }), {
          status: nextCreateStatus, headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(JSON.stringify({ name: createPayload.name, role: createPayload.role }), {
        status: 201, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
});

test('submitting the create form appends the new row without a page reload', async () => {
  const user = userEvent.setup();
  render(<AccountsSection />);

  await screen.findByText('No accounts yet.');
  await user.type(screen.getByLabelText(/^Name/), 'bob');
  await user.type(screen.getByLabelText(/^Password/), 's3cret');
  await user.click(screen.getByRole('button', { name: 'Add account' }));

  assert.ok(await screen.findByText('bob'));
  assert.deepEqual(createPayload, { name: 'bob', password: 's3cret', role: 'viewer' });
});

test('a server rejection surfaces the error instead of a false-positive row', async () => {
  nextCreateStatus = 409;
  const user = userEvent.setup();
  render(<AccountsSection />);

  await screen.findByText('No accounts yet.');
  await user.type(screen.getByLabelText(/^Name/), 'bob');
  await user.type(screen.getByLabelText(/^Password/), 's3cret');
  await user.click(screen.getByRole('button', { name: 'Add account' }));

  assert.ok(await screen.findByText('user already exists'));
  assert.equal(screen.queryByText('bob'), null);
});
