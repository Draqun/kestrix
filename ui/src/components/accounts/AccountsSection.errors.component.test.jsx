import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import AccountsSection from './AccountsSection.jsx';

vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({ roleAtLeast: () => true }),
}));

test('a failed initial list load surfaces the error', async () => {
  globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ error: 'store unavailable' }), {
    status: 503, headers: { 'Content-Type': 'application/json' },
  }));

  render(<AccountsSection />);

  assert.ok(await screen.findByText(/List accounts failed/));
});

test('an account with no grants field renders without a grant chip', async () => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer' }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });

  render(<AccountsSection />);

  assert.ok(await screen.findByText('alice'));
  assert.equal(screen.queryByText(/:/), null);
});

test('a failed role change surfaces the error and leaves the row unchanged', async () => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users/alice/role' && options.method === 'PUT') {
      return new Response(JSON.stringify({ error: 'role must be viewer, operator, or admin' }), {
        status: 400, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
  vi.stubGlobal('confirm', () => true);

  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.click(row.getByLabelText('Role'));
  await user.click(await screen.findByRole('option', { name: 'admin' }));

  assert.ok(await screen.findByText('role must be viewer, operator, or admin'));
  assert.equal(row.getByLabelText('Role').textContent, 'viewer');
});

test('cancelling a role-change confirmation sends no request', async () => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });
  vi.stubGlobal('confirm', () => false);

  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.click(row.getByLabelText('Role'));
  await user.click(await screen.findByRole('option', { name: 'admin' }));

  assert.equal(globalThis.fetch.mock.calls.length, 1);
  assert.equal(row.getByLabelText('Role').textContent, 'viewer');
});

test('clicking add grant with no project typed sends no request', async () => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });

  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.click(row.getByLabelText(/Add grant/));

  assert.equal(globalThis.fetch.mock.calls.length, 1);
});

test('a failed grant add surfaces the error', async () => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users/alice/grants' && options.method === 'PUT') {
      return new Response(JSON.stringify({ error: 'project not found' }), {
        status: 400, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });

  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.type(row.getByLabelText('Project'), 'demo');
  await user.click(row.getByLabelText(/Add grant/));

  assert.ok(await screen.findByText('project not found'));
  assert.equal(screen.queryByText('demo: viewer'), null);
});

test('a failed grant removal surfaces the error', async () => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({
        items: [{ name: 'alice', role: 'viewer', grants: [{ project_name: 'demo', role: 'operator' }] }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/users/alice/grants' && options.method === 'PUT') {
      return new Response(JSON.stringify({ error: 'update failed' }), {
        status: 500, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });

  const user = userEvent.setup();
  render(<AccountsSection />);

  assert.ok(await screen.findByText('demo: operator'));
  await user.click(screen.getByTestId('CancelIcon'));

  assert.ok(await screen.findByText('update failed'));
  assert.ok(screen.getByText('demo: operator'));
});

test('cancelling the delete confirmation sends no request', async () => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });
  vi.stubGlobal('confirm', () => false);

  const user = userEvent.setup();
  render(<AccountsSection />);

  await screen.findByText('alice');
  await user.click(screen.getByRole('button', { name: 'Delete' }));

  assert.equal(globalThis.fetch.mock.calls.length, 1);
});

test('a failed delete surfaces the error and keeps the row', async () => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users/alice' && options.method === 'DELETE') {
      return new Response(JSON.stringify({ error: 'user not found' }), {
        status: 404, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
  vi.stubGlobal('confirm', () => true);

  const user = userEvent.setup();
  render(<AccountsSection />);

  await screen.findByText('alice');
  await user.click(screen.getByRole('button', { name: 'Delete' }));

  assert.ok(await screen.findByText('user not found'));
  assert.ok(screen.getByText('alice'));
});

test('picking a role in the create form and the grant-role select updates each draft', async () => {
  let grants = [];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'viewer', grants }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users' && options.method === 'POST') {
      const body = JSON.parse(options.body);
      return new Response(JSON.stringify({ name: body.name, role: body.role }), {
        status: 201, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users/alice/grants' && options.method === 'PUT') {
      grants = JSON.parse(options.body).grants;
      return new Response(JSON.stringify({ name: 'alice', grants }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });

  const user = userEvent.setup();
  render(<AccountsSection />);

  await screen.findByText('alice');

  const createRoleSelect = screen.getAllByLabelText('Role')[0];
  await user.click(createRoleSelect);
  await user.click(await screen.findByRole('option', { name: 'operator' }));
  assert.equal(createRoleSelect.textContent, 'operator');

  const row = within(screen.getByTestId('account-row-alice'));
  await user.click(row.getByLabelText('Grant role'));
  await user.click(await screen.findByRole('option', { name: 'admin' }));
  await user.type(row.getByLabelText('Project'), 'demo');
  await user.click(row.getByLabelText(/Add grant/));

  assert.ok(await screen.findByText('demo: admin'));
});
