import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import AccountsSection from './AccountsSection.jsx';

vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({ roleAtLeast: () => true }),
}));

let listCalls;
let rolePayload;
let grantsPayload;

beforeEach(() => {
  listCalls = 0;
  rolePayload = null;
  grantsPayload = null;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      listCalls++;
      if (listCalls === 1) {
        return new Response(JSON.stringify({
          items: [{ name: 'alice', role: 'viewer', grants: [] }],
        }), { status: 200, headers: { 'Content-Type': 'application/json' } });
      }
      return new Response(JSON.stringify({
        items: [{
          name: 'alice',
          role: rolePayload ? rolePayload.role : 'viewer',
          grants: grantsPayload ? grantsPayload.grants : [],
        }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/users/alice/role' && options.method === 'PUT') {
      rolePayload = JSON.parse(options.body);
      return new Response(JSON.stringify({ name: 'alice', role: rolePayload.role }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/users/alice/grants' && options.method === 'PUT') {
      grantsPayload = JSON.parse(options.body);
      return new Response(JSON.stringify({ name: 'alice', grants: grantsPayload.grants }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
  vi.stubGlobal('confirm', () => true);
});

test('changing an account role updates the row once the request succeeds', async () => {
  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.click(row.getByLabelText('Role'));
  await user.click(await screen.findByRole('option', { name: 'admin' }));

  await vi.waitFor(() => assert.deepEqual(rolePayload, { role: 'admin' }));
  await vi.waitFor(() => assert.equal(row.getByLabelText('Role').textContent, 'admin'));
});

test('adding a project grant shows the new grant once the request succeeds', async () => {
  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.type(row.getByLabelText('Project'), 'demo');
  await user.click(row.getByLabelText(/Add grant/));

  await vi.waitFor(() => assert.deepEqual(grantsPayload, { grants: [{ project_name: 'demo', role: 'viewer' }] }));
  assert.ok(await screen.findByText('demo: viewer'));
});

test('adding a grant trims the project name before sending and deduping', async () => {
  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.type(row.getByLabelText('Project'), '  demo  ');
  await user.click(row.getByLabelText(/Add grant/));

  await vi.waitFor(() => assert.deepEqual(grantsPayload, { grants: [{ project_name: 'demo', role: 'viewer' }] }));
});

test('a row\'s controls disable while its mutation is in flight, then re-enable', async () => {
  let resolvePut;
  const putResponse = new Promise((resolve) => { resolvePut = resolve; });
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({
        items: [{ name: 'alice', role: 'viewer', grants: grantsPayload ? grantsPayload.grants : [] }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/users/alice/grants' && options.method === 'PUT') {
      grantsPayload = JSON.parse(options.body);
      return putResponse;
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });

  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.type(row.getByLabelText('Project'), 'demo');
  await user.click(row.getByLabelText(/Add grant/));

  // The request is still pending - a second click here must not be able to
  // fire a concurrent, stale-snapshot mutation (see R-race in mr review).
  await vi.waitFor(() => assert.equal(row.getByLabelText(/Add grant/).disabled, true));
  assert.equal(row.getByLabelText('Role').getAttribute('aria-disabled'), 'true');

  resolvePut(new Response(JSON.stringify({ name: 'alice', grants: grantsPayload.grants }), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  }));

  await vi.waitFor(() => assert.equal(row.getByLabelText(/Add grant/).disabled, false));
});

test('removing a project grant clears it once the request succeeds', async () => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/users' && !options.method) {
      return new Response(JSON.stringify({
        items: [{
          name: 'alice',
          role: 'viewer',
          grants: grantsPayload ? grantsPayload.grants : [{ project_name: 'demo', role: 'operator' }],
        }],
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/users/alice/grants' && options.method === 'PUT') {
      grantsPayload = JSON.parse(options.body);
      return new Response(JSON.stringify({ name: 'alice', grants: grantsPayload.grants }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });

  const user = userEvent.setup();
  render(<AccountsSection />);

  assert.ok(await screen.findByText('demo: operator'));
  await user.click(screen.getByTestId('CancelIcon'));

  await vi.waitFor(() => assert.deepEqual(grantsPayload, { grants: [] }));
  await vi.waitFor(() => assert.equal(screen.queryByText('demo: operator') === null, true));
});
