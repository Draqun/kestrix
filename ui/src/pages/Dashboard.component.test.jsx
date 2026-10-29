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


test('dashboard has a page title and a direct route to pipelines', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    project_count: 0, run_stats: {}, recent_runs: [], pg_available: true,
  }));
  render(<MemoryRouter><Dashboard /></MemoryRouter>);
  assert.ok(screen.getByRole('heading', { name: 'Dashboard', level: 1 }));
  assert.equal(screen.getByRole('link', { name: 'View pipelines' }).getAttribute('href'), '/pipelines');
  assert.ok(await screen.findByText('No runs yet. Trigger a pipeline to see results here.'));
});
