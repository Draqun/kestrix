import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { test, vi } from 'vitest';
import { RunDetail, RunList } from './Pipelines.jsx';

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

test('run detail shows who started the run', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    id: 1, project_name: 'demo', status: 'passed', started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice', task_results: [],
  }));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={1} onBack={() => {}} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText(/by alice/));
});

test('run detail shows "unknown" for a legacy run with no starter', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    id: 2, project_name: 'demo', status: 'passed', started_at: '2026-01-01T00:00:00Z',
    started_by: '', task_results: [],
  }));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={2} onBack={() => {}} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText(/by unknown/));
});

test('run detail shows who canceled the run', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    id: 3, project_name: 'demo', status: 'canceled', started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice', canceled_by: 'user:bob', task_results: [],
  }));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={3} onBack={() => {}} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText(/canceled by bob/));
});

test('approved task shows its approver', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    id: 4, project_name: 'demo', status: 'passed', started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    task_results: [
      { name: 'deploy', status: 'passed', approved_by: 'user:carol', approved_at: '2026-01-01T00:05:00Z' },
    ],
  }));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={4} onBack={() => {}} />
    </MemoryRouter>,
  );

  const user = userEvent.setup();
  await user.click(await screen.findByText('deploy'));

  assert.ok(await screen.findByText(/approved by carol/));
});

test('unapproved task shows no approver', async () => {
  globalThis.fetch = vi.fn(async () => jsonResponse({
    id: 5, project_name: 'demo', status: 'running', started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    task_results: [{ name: 'build', status: 'running' }],
  }));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={5} onBack={() => {}} />
    </MemoryRouter>,
  );

  const user = userEvent.setup();
  await user.click(await screen.findByText('build'));

  assert.equal(screen.queryByText(/approved by/), null);
});

test('runs list shows who started each run', async () => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/runs') {
      return jsonResponse([
        { id: 1, project_name: 'demo', status: 'passed', started_at: '2026-01-01T00:00:00Z', started_by: 'user:alice' },
      ]);
    }
    throw new Error(`unexpected request: ${url}`);
  });

  render(
    <MemoryRouter initialEntries={['/pipelines']}>
      <RunList onNewRun={() => {}} onOpenRun={() => {}} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText(/by alice/));
});
