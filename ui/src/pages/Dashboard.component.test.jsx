import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { test, vi } from 'vitest';
import Dashboard from './Dashboard.jsx';

function jsonResponse(body) {
  return new Response(JSON.stringify(body), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  });
}

test('dashboard shows a queued run counter', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    project_count: 1,
    run_stats: { passed: 2, failed: 1, running: 1, queued: 3, canceled: 0 },
    recent_runs: [],
  }));

  render(
    <MemoryRouter>
      <Dashboard />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText('Queued'));
  assert.ok(await screen.findByText('3'));
});
