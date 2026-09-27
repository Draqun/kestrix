import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import RunnersSection from './RunnersSection.jsx';

const authState = vi.hoisted(() => ({ role: 'admin' }));
vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({
    roleAtLeast: (min) => {
      const rank = { viewer: 1, operator: 2, admin: 3 };
      return (rank[authState.role] || 0) >= (rank[min] || 0);
    },
  }),
}));

const json = (body, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { 'Content-Type': 'application/json' },
});

let runners;
let requests;

beforeEach(() => {
  authState.role = 'admin';
  runners = [{ id: 1, name: 'gpu-box', tags: ['gpu', 'linux'], status: 'online', last_seen_at: '2026-09-26T10:00:00Z', created_at: '2026-09-26T09:00:00Z' }];
  requests = [];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    requests.push({ url, method: options.method || 'GET', body: options.body });
    if (url === '/api/runners' && !options.method) return json({ items: runners });
    if (url === '/api/runners' && options.method === 'POST') {
      const { name, tags } = JSON.parse(options.body);
      runners = [...runners, { id: 2, name, tags, status: 'never seen', created_at: '2026-09-26T11:00:00Z' }];
      return json({ id: 2, name, tags, token: 'tok-shown-once' }, 201);
    }
    if (url === '/api/runners/1' && options.method === 'DELETE') {
      runners = runners.filter((r) => r.id !== 1);
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
  vi.stubGlobal('confirm', () => true);
});

test('a non-admin session renders nothing and never requests the runner list', async () => {
  authState.role = 'operator';
  const { container } = render(<RunnersSection />);
  assert.equal(container.firstChild, null);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(globalThis.fetch.mock.calls.length, 0);
});

test('an admin sees each runner with its tags and liveness', async () => {
  render(<RunnersSection />);
  const row = await screen.findByTestId('runner-row-1');
  assert.ok(within(row).getByText('gpu-box'));
  assert.ok(within(row).getByText('gpu'));
  assert.ok(within(row).getByText('linux'));
  assert.ok(within(row).getByText('online'));
});

test('registering shows the token once, with the runner command, until the dialog closes', async () => {
  const user = userEvent.setup();
  render(<RunnersSection />);
  await screen.findByTestId('runner-row-1');

  await user.type(screen.getByLabelText(/Runner name/), 'plain-box');
  await user.type(screen.getByLabelText(/Tags/), 'ARM64, linux');
  await user.click(screen.getByRole('button', { name: 'Register runner' }));

  const command = await screen.findByTestId('runner-command');
  assert.match(command.textContent, /^kestrix runner --server \S+ --token tok-shown-once$/);
  const post = requests.find((r) => r.method === 'POST');
  assert.deepEqual(JSON.parse(post.body), { name: 'plain-box', tags: ['ARM64', 'linux'] });
  const newRow = await screen.findByTestId('runner-row-2');
  assert.ok(within(newRow).getByText('never seen'));

  await user.click(screen.getByRole('button', { name: 'Done' }));
  // Compare a boolean, never the element: a failing assert.equal(element, null)
  // inspects the whole jsdom node graph on every waitFor retry and runs out of memory.
  await vi.waitFor(() => assert.equal(screen.queryByTestId('runner-command') === null, true));
  assert.equal(document.body.textContent.includes('tok-shown-once'), false);
});

test('revoking asks for confirmation, calls the API, and removes the row', async () => {
  const user = userEvent.setup();
  render(<RunnersSection />);
  const row = await screen.findByTestId('runner-row-1');
  await user.click(within(row).getByRole('button', { name: 'Revoke' }));
  await vi.waitFor(() => assert.ok(requests.some((r) => r.method === 'DELETE' && r.url === '/api/runners/1')));
  await vi.waitFor(() => assert.equal(screen.queryByTestId('runner-row-1') === null, true));
});

test('while one revoke is in flight, every Revoke button is disabled', async () => {
  runners = [...runners, { id: 2, name: 'plain-box', tags: [], status: 'online', created_at: '2026-09-26T09:30:00Z' }];
  let release;
  const held = new Promise((resolve) => { release = resolve; });
  const baseFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (options.method === 'DELETE') {
      await held;
      return new Response(null, { status: 204 });
    }
    return baseFetch(url, options);
  });
  const user = userEvent.setup();
  render(<RunnersSection />);
  const first = await screen.findByTestId('runner-row-1');
  const second = await screen.findByTestId('runner-row-2');
  await user.click(within(first).getByRole('button', { name: 'Revoke' }));
  await vi.waitFor(() => assert.equal(within(second).getByRole('button', { name: 'Revoke' }).disabled, true));
  release();
  await vi.waitFor(() => assert.equal(within(second).getByRole('button', { name: 'Revoke' }).disabled, false));
});

test('a rejected registration shows the backend error and no token', async () => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (!options.method) return json({ items: [] });
    return json({ error: 'invalid runner tag "has space"' }, 400);
  });
  const user = userEvent.setup();
  render(<RunnersSection />);
  await screen.findByText('No runners registered.');
  await user.type(screen.getByLabelText(/Runner name/), 'x');
  await user.click(screen.getByRole('button', { name: 'Register runner' }));
  assert.ok(await screen.findByText('invalid runner tag "has space"'));
  assert.equal(screen.queryByTestId('runner-command') === null, true);
});
