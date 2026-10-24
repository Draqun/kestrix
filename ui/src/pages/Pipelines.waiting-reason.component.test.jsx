import assert from 'node:assert/strict';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import { RunDetail } from './Pipelines.jsx';

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

beforeEach(() => {
  authState.role = 'operator';
});

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status, headers: { 'Content-Type': 'application/json' },
  });
}

test('run detail shows warning alert with waiting_reason for an unmatched pending task', async () => {
  const reasonText = 'no registered runner has tags: arm64, gpu';
  const run = {
    id: 12,
    project_name: 'demo',
    status: 'running',
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: { tasks: [{ name: 'gpu-task' }, { name: 'other-task' }] },
    task_results: [
      { name: 'gpu-task', status: 'pending', waiting_reason: reasonText },
      { name: 'other-task', status: 'pending' },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={12} onBack={() => {}} />
    </MemoryRouter>,
  );

  // Compare booleans inside waitFor
  await waitFor(() => {
    const present = Boolean(screen.queryByText(reasonText));
    assert.equal(present, true);
  });

  // Verify it renders as a warning alert
  const alert = screen.getByText(reasonText).closest('.MuiAlert-root');
  assert.ok(alert);
  assert.ok(alert.classList.contains('MuiAlert-colorWarning'));

  // Click on the job to inspect detail pane
  const jobBtn = screen.getByRole('button', { name: new RegExp('gpu-task', 'i') });
  fireEvent.click(jobBtn);

  await waitFor(() => {
    const alerts = screen.queryAllByText(reasonText);
    assert.equal(alerts.length >= 2, true);
  });
});

test('run detail does not render waiting_reason warning alert when absent', async () => {
  const run = {
    id: 13,
    project_name: 'demo',
    status: 'running',
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: { tasks: [{ name: 'normal-task' }] },
    task_results: [
      { name: 'normal-task', status: 'pending' },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={13} onBack={() => {}} />
    </MemoryRouter>,
  );

  await waitFor(() => {
    const taskPresent = Boolean(screen.queryByText('normal-task'));
    assert.equal(taskPresent, true);
  });

  const alerts = document.querySelectorAll('.MuiAlert-colorWarning');
  assert.equal(alerts.length, 0);
});
