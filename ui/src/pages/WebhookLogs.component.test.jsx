import assert from 'node:assert/strict';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import WebhookLogs from './WebhookLogs.jsx';

let requests;

// respond serves the paginated envelope the server returns, echoing back the
// filters so assertions can check what the page actually asked for.
function respond(items) {
  return new Response(JSON.stringify({
    items,
    pagination: { total: items.length, limit: 25, offset: 0, next: null, prev: null },
  }), { status: 200, headers: { 'Content-Type': 'application/json' } });
}

const triggeredEvent = {
  id: 1,
  received_at: '2026-09-01T10:00:00Z',
  project_name: 'web',
  repo: 'org/web',
  branch: 'main',
  revision: '0123456789abcdef',
  commits: 2,
  triggered_tasks: ['build_auth'],
  status: 'triggered',
  dispatched_pipeline_id: 42,
  dispatched_pipeline_url: 'https://gitlab.example/org/web/-/pipelines/42',
};

beforeEach(() => {
  requests = [];
  globalThis.fetch = vi.fn(async (url) => {
    requests.push(url);
    return respond([triggeredEvent]);
  });
});

test('WebhookLogs renders live events from the server', async () => {
  render(<WebhookLogs />);

  await screen.findByText('org/web');
  assert.ok(screen.getByText('build_auth'));
  assert.ok(screen.getByText('#42'));
  // The page must reach the API through the shared base URL, never a
  // hard-coded development host.
  assert.ok(requests.every(url => !String(url).includes('localhost:8080')));
  assert.ok(String(requests[0]).startsWith('/api/logs'));
});

test('WebhookLogs shows an empty state instead of placeholder rows', async () => {
  globalThis.fetch = vi.fn(async () => respond([]));

  render(<WebhookLogs />);

  await screen.findByText('No webhook events recorded yet.');
  assert.equal(screen.queryByText('auth_service'), null);
  assert.equal(screen.queryByText('billing_service'), null);
});

test('WebhookLogs shows an error state when the request fails', async () => {
  globalThis.fetch = vi.fn(async () => new Response('boom', { status: 500, statusText: 'Internal Server Error' }));

  render(<WebhookLogs />);

  await screen.findByText(/Webhook history failed/);
  assert.equal(screen.queryByText('org/web'), null);
});

test('WebhookLogs requests the filtered page when a status is selected', async () => {
  const user = userEvent.setup();
  render(<WebhookLogs />);
  await screen.findByText('org/web');

  await user.click(screen.getByLabelText('Status'));
  await user.click(await screen.findByRole('option', { name: 'error' }));

  await waitFor(() => {
    assert.ok(requests.some(url => String(url).includes('status=error')));
  });
});

test('WebhookLogs debounces the project filter into a single request', async () => {
  const user = userEvent.setup();
  render(<WebhookLogs />);

  await screen.findByText('org/web');
  const before = requests.length;

  await user.type(screen.getByLabelText('Project'), 'billing');

  // Seven keystrokes must not become seven requests.
  await waitFor(() => {
    assert.ok(requests.some(url => String(url).includes('project=billing')));
  });
  assert.equal(requests.length - before, 1);
});

test('WebhookLogs ignores a stale response that resolves after a newer one', async () => {
  const user = userEvent.setup();
  const unfiltered = { ...triggeredEvent, id: 1, repo: 'org/unfiltered' };
  const filtered = { ...triggeredEvent, id: 2, repo: 'org/billing', project_name: 'billing' };

  let releaseFirst;
  const firstResponse = new Promise((resolve) => { releaseFirst = resolve; });
  globalThis.fetch = vi.fn(async (url) => {
    requests.push(url);
    // The page-load request stays in flight until the filtered one has landed.
    if (!String(url).includes('project=billing')) {
      await firstResponse;
      return respond([unfiltered]);
    }
    return respond([filtered]);
  });

  render(<WebhookLogs />);
  await user.type(screen.getByLabelText('Project'), 'billing');
  await screen.findByText('org/billing');

  releaseFirst();
  await waitFor(() => {
    assert.ok(requests.some(url => String(url).includes('project=billing')));
  });

  // The older unfiltered response must not repopulate the table.
  assert.equal(screen.queryByText('org/unfiltered'), null);
  assert.ok(screen.getByText('org/billing'));
});
