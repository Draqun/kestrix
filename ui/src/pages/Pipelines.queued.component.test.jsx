import assert from 'node:assert/strict';
import { act, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, test, vi } from 'vitest';
import { RunDetail } from './Pipelines.jsx';

afterEach(() => {
  vi.useRealTimers();
});

const authState = vi.hoisted(() => ({ role: 'operator' }));
vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({
    name: 'test-user',
    role: authState.role,
    logout: () => {},
    roleAtLeast: (min) => {
      const rank = { viewer: 1, operator: 2, admin: 3 };
      return (rank[authState.role] || 0) >= (rank[min] || 0);
    },
  }),
}));

function jsonResponse(body) {
  return new Response(JSON.stringify(body), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  });
}

test('run detail shows the queued status and offers cancel', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    id: 5, project_name: 'demo', status: 'queued', started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice', task_results: [],
  }));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={5} onBack={() => {}} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText('QUEUED'));
  assert.ok(await screen.findByRole('button', { name: /cancel/i }));
});

// REVIEW.md R18: the poll-while-busy effect was widened to include
// 'queued' so a queued run's detail view keeps refreshing, but no test
// asserted the polling interval actually fires for a queued run.
test('queued run detail keeps polling for updates', async () => {
  vi.useFakeTimers();
  let calls = 0;
  globalThis.fetch = vi.fn(async () => {
    calls++;
    return jsonResponse({
      id: 6, project_name: 'demo', status: 'queued', started_at: '2026-01-01T00:00:00Z',
      started_by: 'user:alice', task_results: [],
    });
  });

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={6} onBack={() => {}} />
    </MemoryRouter>,
  );

  // Flush the initial load() triggered synchronously by mount, before any
  // timer advances.
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  assert.equal(calls, 1, 'expected exactly one fetch before any interval tick');

  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
  });
  assert.equal(calls, 2, 'expected the queued run detail to poll again after the 1500ms interval');
});
