import assert from 'node:assert/strict';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import { RunDetail } from './Pipelines.jsx';
import SkipReasonPanel from '../components/SkipReasonPanel.jsx';

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

test('run detail allows inspecting trigger-skipped task with trigger expression and terms', async () => {
  const run = {
    id: 15,
    project_name: 'multi-repo',
    status: 'passed',
    changed: ['infra'],
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: {
      tasks: [
        { name: 'deploy-infra', trigger: 'repo.infra.changed' },
        { name: 'test-backend', trigger: 'repo.backend.changed && !repo.docs.changed' },
      ],
    },
    task_results: [
      { name: 'deploy-infra', status: 'passed', duration: 1000 },
      {
        name: 'test-backend',
        status: 'skipped',
        error: 'trigger expression not satisfied',
        skip_reason: {
          category: 'trigger_unsatisfied',
          summary: 'Trigger condition not satisfied',
          trigger_evaluation: {
            expression: 'repo.backend.changed && !repo.docs.changed',
            changed_repos: ['infra'],
            terms: {
              'repo.backend.changed': false,
              'repo.docs.changed': false,
            },
            satisfied: false,
          },
        },
      },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="multi-repo" runId={15} onBack={() => {}} />
    </MemoryRouter>,
  );

  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('test-backend')), true);
  });

  // Click on the skipped task
  const jobBtn = screen.getByRole('button', { name: /test-backend/i });
  fireEvent.click(jobBtn);

  // Verify Trigger condition section is displayed
  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('Trigger Condition')), true);
    assert.equal(Boolean(screen.getByText('repo.backend.changed && !repo.docs.changed')), true);
    assert.equal(Boolean(screen.getByText('repo.backend.changed')), true);
  });
});

test('run detail allows inspecting dependency-skipped task with failed dependencies', async () => {
  const run = {
    id: 16,
    project_name: 'multi-repo',
    status: 'failed',
    changed: ['backend'],
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: {
      tasks: [
        { name: 'build', trigger: 'repo.backend.changed' },
        { name: 'deploy', trigger: 'repo.backend.changed', depends_on: ['build'] },
      ],
    },
    task_results: [
      { name: 'build', status: 'failed', error: 'compilation error' },
      {
        name: 'deploy',
        status: 'skipped',
        error: 'dependency failed',
        skip_reason: {
          category: 'dependency_failed',
          summary: 'Dependency build failed',
          failed_dependencies: ['build'],
        },
      },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="multi-repo" runId={16} onBack={() => {}} />
    </MemoryRouter>,
  );

  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('deploy')), true);
  });

  // Click on the deploy task
  const jobBtn = screen.getByRole('button', { name: /deploy/i });
  fireEvent.click(jobBtn);

  await waitFor(() => {
    assert.equal(screen.getAllByText(/Dependency build failed/i).length >= 1, true);
    assert.equal(Boolean(screen.getByText(/Failed upstream dependencies: build/i)), true);
  });
});

test('SkipReasonPanel returns null when job is not skipped or missing', () => {
  const { container: c1 } = render(<SkipReasonPanel job={null} />);
  assert.equal(c1.firstChild, null);

  const { container: c2 } = render(<SkipReasonPanel job={{ status: 'passed' }} />);
  assert.equal(c2.firstChild, null);
});

test('run detail allows inspecting dependency_canceled task', async () => {
  const run = {
    id: 17,
    project_name: 'multi-repo',
    status: 'canceled',
    changed: ['backend'],
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: {
      tasks: [
        { name: 'build', trigger: 'repo.backend.changed' },
        { name: 'deploy', trigger: 'repo.backend.changed', depends_on: ['build'] },
      ],
    },
    task_results: [
      { name: 'build', status: 'canceled', error: 'canceled' },
      {
        name: 'deploy',
        status: 'skipped',
        error: 'dependency failed',
        skip_reason: {
          category: 'dependency_canceled',
          summary: 'Dependency build was canceled',
          failed_dependencies: ['build'],
        },
      },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="multi-repo" runId={17} onBack={() => {}} />
    </MemoryRouter>,
  );

  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('deploy')), true);
  });

  fireEvent.click(screen.getByRole('button', { name: /deploy/i }));

  await waitFor(() => {
    assert.equal(screen.getAllByText(/Dependency build was canceled/i).length >= 1, true);
    assert.equal(Boolean(screen.getByText(/Failed upstream dependencies: build/i)), true);
  });

  const chip = screen.getByText('build: canceled');
  fireEvent.click(chip);
  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('(No output)')), true);
  });
});

test('run detail renders run_canceled task and fallback summary when skip_reason is absent', async () => {
  const run = {
    id: 18,
    project_name: 'multi-repo',
    status: 'canceled',
    changed: [],
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: {
      tasks: [
        { name: 'task-rc', trigger: 'true' },
        { name: 'task-fallback', trigger: 'true' },
      ],
    },
    task_results: [
      {
        name: 'task-rc',
        status: 'skipped',
        skip_reason: {
          category: 'run_canceled',
          summary: 'Run was canceled',
        },
      },
      {
        name: 'task-fallback',
        status: 'skipped',
        error: 'Legacy skip message without skip_reason',
      },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="multi-repo" runId={18} onBack={() => {}} />
    </MemoryRouter>,
  );

  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('task-rc')), true);
  });

  fireEvent.click(screen.getByRole('button', { name: /task-rc/i }));
  await waitFor(() => {
    assert.equal(screen.getAllByText(/Run was canceled/i).length >= 1, true);
    assert.equal(Boolean(screen.getByText('None')), true);
  });

  fireEvent.click(screen.getByRole('button', { name: /task-fallback/i }));
  await waitFor(() => {
    assert.equal(screen.getAllByText(/Legacy skip message without skip_reason/i).length >= 1, true);
  });
});

test('trigger-skipped manual task does not show manual approval button', async () => {
  const run = {
    id: 19,
    project_name: 'multi-repo',
    status: 'running',
    changed: ['other'],
    started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: {
      tasks: [
        { name: 'manual-gate-ready', manual: true },
        { name: 'manual-gate-skipped', manual: true, trigger: 'repo.app.changed' },
      ],
    },
    task_results: [
      {
        name: 'manual-gate-ready',
        status: 'manual',
      },
      {
        name: 'manual-gate-skipped',
        status: 'skipped',
        skip_reason: {
          category: 'trigger_unsatisfied',
          summary: 'Trigger condition not satisfied',
        },
      },
    ],
  };

  globalThis.fetch = vi.fn(async () => jsonResponse(run));

  render(
    <MemoryRouter>
      <RunDetail projectName="multi-repo" runId={19} onBack={() => {}} />
    </MemoryRouter>,
  );

  await waitFor(() => {
    assert.equal(Boolean(screen.getByText('manual-gate-ready')), true);
    assert.equal(Boolean(screen.getByText('manual-gate-skipped')), true);
  });

  const readyItem = screen.getByText('manual-gate-ready').closest('li');
  const skippedItem = screen.getByText('manual-gate-skipped').closest('li');

  // Positive control: plain manual task has the list-row "Run" button
  await waitFor(() => {
    assert.equal(Boolean(within(readyItem).getByRole('button', { name: /^run$/i })), true);
    // Negative control: trigger-skipped manual task does not have the list-row "Run" button
    assert.equal(within(skippedItem).queryByRole('button', { name: /^run$/i }) === null, true);
  });

  // Verify detail panel button behavior as well:
  // Selecting manual-gate-ready shows "Run manual job"
  fireEvent.click(screen.getByRole('button', { name: /manual-gate-ready/i }));
  await waitFor(() => {
    assert.equal(Boolean(screen.getByRole('button', { name: /run manual job/i })), true);
  });

  // Selecting manual-gate-skipped does NOT show "Run manual job"
  fireEvent.click(screen.getByRole('button', { name: /manual-gate-skipped/i }));
  await waitFor(() => {
    assert.equal(screen.queryByRole('button', { name: /run manual job/i }) === null, true);
  });
});
