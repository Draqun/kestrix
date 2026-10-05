import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { test, vi } from 'vitest';
import { RunDetail, RunList } from './Pipelines.jsx';

vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({
    name: 'test-user',
    role: 'operator',
    logout: () => {},
    roleAtLeast: () => true,
  }),
}));

function jsonResponse(body) {
  return new Response(JSON.stringify(body), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  });
}

const base = {
  project_name: 'demo', status: 'passed', started_at: '2026-01-01T00:00:00Z',
  started_by: 'user:alice', task_results: [],
};
// A single-task re-run records its task and no changed repositories.
const rerun = { ...base, id: 7, changed: [], rerun_task: 'build' };
const whole = { ...base, id: 8, changed: ['app'], rerun_task: '' };

function renderDetail(run) {
  globalThis.fetch = vi.fn(async () => jsonResponse(run));
  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={run.id} onBack={() => {}} />
    </MemoryRouter>,
  );
}

test('run detail labels a single-task re-run with its task', async () => {
  renderDetail(rerun);

  assert.ok(await screen.findByText('Re-run: build'));
  assert.equal(screen.queryByText(/Changed:/) === null, true);
});

test('run detail keeps the changed label for a whole run', async () => {
  renderDetail(whole);

  assert.ok(await screen.findByText('Changed: app'));
  assert.equal(screen.queryByText(/Re-run:/) === null, true);
});

test('run list labels each run by what started it', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse([rerun, whole]));

  render(
    <MemoryRouter>
      <RunList onNewRun={() => {}} onOpenRun={() => {}} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText('Re-run: build'));
  assert.ok(await screen.findByText('Changed: app'));
  assert.equal(screen.queryByText('Changed: build') === null, true);
});
