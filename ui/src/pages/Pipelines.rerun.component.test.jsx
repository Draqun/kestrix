import assert from 'node:assert/strict';
import { useState } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import { RunDetail } from './Pipelines.jsx';

// ui-single-task-rerun: the Re-run action on a finished run's tasks.

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

// A finished run with a failed task and a manual task still waiting for
// approval (skipped, no dependency failure).
function finishedRun(overrides = {}) {
  return {
    id: 5, project_name: 'demo', status: 'failed', started_at: '2026-01-01T00:00:00Z',
    started_by: 'user:alice',
    pipeline_def: { tasks: [{ name: 'build' }, { name: 'deploy', manual: true }] },
    task_results: [
      { name: 'build', status: 'failed', error: 'exit 1' },
      { name: 'deploy', status: 'skipped' },
    ],
    ...overrides,
  };
}

function mockApi({ run = finishedRun(), rerun, runs = {} } = {}) {
  const calls = [];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    calls.push({ url, method: options.method || 'GET' });
    if (options.method === 'POST' && url.includes('/tasks/')) return rerun();
    const id = Number(url.split('/').pop());
    return jsonResponse(runs[id] || run);
  });
  return calls;
}

function renderDetail(props = {}) {
  return render(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={5} onBack={() => {}} onRerunStarted={() => {}} {...props} />
    </MemoryRouter>,
  );
}

test('re-run is offered for a failed task but not for a manual-waiting one', async () => {
  mockApi();
  renderDetail();

  assert.ok(await screen.findByText('build'));
  const rerun = await screen.findAllByRole('button', { name: /re-run/i });
  assert.equal(rerun.length, 1);
  const buildItem = screen.getByText('build').closest('li');
  assert.ok(within(buildItem).getByRole('button', { name: /re-run/i }));
  const deployItem = screen.getByText('deploy').closest('li');
  assert.ok(within(deployItem).getByRole('button', { name: /^run$/i }));
  assert.equal(within(deployItem).queryByRole('button', { name: /re-run/i }), null);
});

// Spec scenario "No action for a task that never ran": a task skipped
// because a dependency failed gets neither the approval action nor a
// re-run.
test('re-run is not offered for a task skipped by a failed dependency', async () => {
  mockApi({
    run: finishedRun({
      pipeline_def: { tasks: [{ name: 'build' }, { name: 'package', depends_on: ['build'] }] },
      task_results: [
        { name: 'build', status: 'failed', error: 'exit 1' },
        { name: 'package', status: 'skipped', error: 'dependency failed' },
      ],
    }),
  });
  renderDetail();

  const packageItem = (await screen.findByText('package')).closest('li');
  assert.equal(within(packageItem).queryByRole('button', { name: /re-run|^run$/i }) === null, true);
  assert.equal(screen.getAllByRole('button', { name: /re-run/i }).length, 1);
});

test('re-run is also offered in the selected job header', async () => {
  mockApi();
  renderDetail();

  fireEvent.click(await screen.findByText('build'));
  assert.ok(await screen.findByRole('button', { name: '← DAG' }));
  assert.equal(screen.getAllByRole('button', { name: /re-run/i }).length, 2);
});

test('re-run is not offered while the run is still active', async () => {
  mockApi({ run: finishedRun({ status: 'running', task_results: [{ name: 'build', status: 'failed' }] }) });
  renderDetail();

  assert.ok(await screen.findByText('build'));
  assert.equal(screen.queryByRole('button', { name: /re-run/i }), null);
});

test('re-run is hidden from a viewer', async () => {
  authState.role = 'viewer';
  mockApi();
  renderDetail();

  fireEvent.click(await screen.findByText('build'));
  assert.ok(await screen.findByRole('button', { name: '← DAG' }));
  assert.equal(screen.queryByRole('button', { name: /re-run/i }), null);
});

test('an accepted re-run moves the view to the new run and shows it queued', async () => {
  const calls = mockApi({
    rerun: () => jsonResponse({ run_id: 9, project: 'demo', task: 'build', status: 'queued' }),
    runs: { 9: finishedRun({ id: 9, status: 'queued', task_results: [] }) },
  });
  function Harness() {
    const [runId, setRunId] = useState(5);
    return (
      <MemoryRouter>
        <RunDetail projectName="demo" runId={runId} onBack={() => {}} onRerunStarted={setRunId} />
      </MemoryRouter>
    );
  }
  render(<Harness />);

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));

  assert.ok(await screen.findByText('Run #9'));
  assert.equal(screen.queryAllByRole('button', { name: /re-run/i }).length, 0);
  assert.ok(await screen.findByText('QUEUED'));
  assert.ok(calls.some(c => c.method === 'POST' && c.url === '/api/projects/demo/tasks/build/run'));
  assert.equal(screen.queryByRole('button', { name: /re-run/i }), null);
});

test('a rejected re-run shows the error and stays on the current run', async () => {
  mockApi({ rerun: () => jsonResponse({ error: 'task "build" not found' }, 404) });
  const onRerunStarted = vi.fn();
  renderDetail({ onRerunStarted });

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));

  assert.ok(await screen.findByText('task "build" not found'));
  assert.ok(screen.getByText('Run #5'));
  assert.equal(onRerunStarted.mock.calls.length, 0);
  await waitFor(() => assert.ok(screen.getByRole('button', { name: /re-run/i }).disabled === false));
});

test('an accepted re-run without a run id stays on the current run', async () => {
  mockApi({ rerun: () => jsonResponse({ status: 'running' }) });
  renderDetail();

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));

  assert.ok(await screen.findByText('Re-run started but no run_id returned'));
  assert.ok(screen.getByText('Run #5'));
});

function deferred() {
  let resolve;
  const promise = new Promise((r) => { resolve = r; });
  return { promise, resolve };
}

const twoFinishedTasks = () => finishedRun({
  pipeline_def: { tasks: [{ name: 'build' }, { name: 'test' }] },
  task_results: [
    { name: 'build', status: 'failed', error: 'exit 1' },
    { name: 'test', status: 'passed' },
  ],
});

// The in-flight state labels the clicked action and disables
// the others, so one click cannot become two runs.
test('a re-run in flight labels its button and disables the others', async () => {
  const pending = deferred();
  mockApi({ run: twoFinishedTasks(), rerun: () => pending.promise });
  renderDetail();

  const [buildRerun] = await screen.findAllByRole('button', { name: /re-run/i });
  fireEvent.click(buildRerun);

  const starting = await screen.findByRole('button', { name: /starting…/i });
  assert.equal(starting.disabled, true);
  const others = screen.getAllByRole('button', { name: /re-run/i });
  assert.equal(others.length, 1);
  assert.equal(others[0].disabled, true);

  pending.resolve(jsonResponse({ run_id: 9, status: 'running' }));
  await waitFor(() => assert.equal(screen.queryByRole('button', { name: /starting…/i }) === null, true));
});

test('a rejected re-run error can be dismissed', async () => {
  mockApi({ rerun: () => jsonResponse({ error: 'no slot today' }, 500) });
  renderDetail();

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));
  assert.ok(await screen.findByText('no slot today'));
  fireEvent.click(screen.getByRole('button', { name: /close/i }));

  await waitFor(() => assert.equal(screen.queryByText('no slot today') === null, true));
});

test('a re-run accepted after the user left the run does not navigate', async () => {
  const pending = deferred();
  mockApi({ rerun: () => pending.promise });
  const onRerunStarted = vi.fn();
  const { unmount } = renderDetail({ onRerunStarted });

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));
  unmount();
  pending.resolve(jsonResponse({ run_id: 9, status: 'running' }));
  await pending.promise;
  await new Promise((r) => setTimeout(r, 0));

  assert.equal(onRerunStarted.mock.calls.length, 0);
});

test('a re-run rejected after the user left the run shows nothing', async () => {
  const pending = deferred();
  mockApi({ rerun: () => pending.promise });
  const { unmount } = renderDetail();

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));
  unmount();
  pending.resolve(jsonResponse({ error: 'too late' }, 500));
  await pending.promise;
  await new Promise((r) => setTimeout(r, 0));

  assert.equal(screen.queryByText('too late') === null, true);
});

test('an accepted re-run with no navigation handler shows no error', async () => {
  mockApi({ rerun: () => jsonResponse({ run_id: 9, status: 'running' }) });
  renderDetail({ onRerunStarted: undefined });

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));

  await waitFor(() => assert.equal(screen.getByRole('button', { name: /re-run/i }).disabled, false));
  assert.equal(screen.queryByText(/no run_id returned/) === null, true);
  assert.ok(screen.getByText('Run #5'));
});

// While the new run loads, the view must never show the previous run's jobs,
// whose Re-run buttons would start a duplicate run.
test('while the new run loads, no job of the previous run is shown', async () => {
  const newRun = deferred();
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (options.method === 'POST') return jsonResponse({ run_id: 9, status: 'running' });
    if (url.endsWith('/runs/9')) return newRun.promise;
    return jsonResponse(finishedRun());
  });
  function Harness() {
    const [runId, setRunId] = useState(5);
    return (
      <MemoryRouter>
        <RunDetail projectName="demo" runId={runId} onBack={() => {}} onRerunStarted={setRunId} />
      </MemoryRouter>
    );
  }
  render(<Harness />);

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));

  assert.ok(await screen.findByText('Run #9'));
  assert.equal(screen.queryByText('build') === null, true);
  assert.equal(screen.queryAllByRole('button', { name: /re-run/i }).length, 0);

  newRun.resolve(jsonResponse(finishedRun({ id: 9, task_results: [{ name: 'lint', status: 'failed' }] })));
  assert.ok(await screen.findByText('lint'));
  assert.equal(screen.getAllByRole('button', { name: /re-run/i }).length, 1);
});

// A load for a run the view has left, resolving after the new run loaded,
// must not replace the new run's detail.
test('a late load for the previous run is discarded', async () => {
  const oldLoad = deferred();
  let oldCalls = 0;
  globalThis.fetch = vi.fn(async (url) => {
    if (url.endsWith('/runs/5')) {
      oldCalls += 1;
      return oldCalls === 1 ? oldLoad.promise : jsonResponse(finishedRun());
    }
    return jsonResponse(finishedRun({ id: 7, task_results: [{ name: 'lint', status: 'failed' }] }));
  });
  const { rerender } = renderDetail();

  rerender(
    <MemoryRouter>
      <RunDetail projectName="demo" runId={7} onBack={() => {}} onRerunStarted={() => {}} />
    </MemoryRouter>,
  );
  assert.ok(await screen.findByText('lint'));

  oldLoad.resolve(jsonResponse(finishedRun()));
  await oldLoad.promise;
  await new Promise((r) => setTimeout(r, 0));

  assert.ok(screen.getByText('lint'));
  assert.equal(screen.queryByText('build') === null, true);
});

function renderRun(runId, props = {}) {
  return (
    <MemoryRouter>
      <RunDetail projectName="demo" runId={runId} onBack={() => {}} onRerunStarted={() => {}} {...props} />
    </MemoryRouter>
  );
}

function runsById(url) {
  const id = Number(url.split('/').pop());
  return jsonResponse(finishedRun({ id, task_results: [{ name: `task-${id}`, status: 'failed' }] }));
}

// A re-run still in flight for the run the user left must not disable, or
// relabel, the Re-run buttons of the run they moved to.
test('moving to another run while a re-run is in flight leaves its buttons usable', async () => {
  const pending = deferred();
  globalThis.fetch = vi.fn(async (url, options = {}) => (options.method === 'POST' ? pending.promise : runsById(url)));
  const { rerender } = render(renderRun(5));

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));
  assert.ok(await screen.findByRole('button', { name: /starting…/i }));
  rerender(renderRun(7));

  assert.ok(await screen.findByText('task-7'));
  const button = screen.getByRole('button', { name: /re-run/i });
  assert.equal(button.disabled, false);
  pending.resolve(jsonResponse({ run_id: 9, status: 'running' }));
});

// RunDetailRoutes keeps RunDetail mounted and only changes runId when the
// route moves to another run: a re-run settling after that must neither
// navigate nor show its error on the run the user moved to.
test('a re-run settling after runId changed does not navigate or show an error', async () => {
  const accepted = deferred();
  const rejected = deferred();
  let posts = 0;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (options.method !== 'POST') return runsById(url);
    posts += 1;
    return posts === 1 ? accepted.promise : rejected.promise;
  });
  const onRerunStarted = vi.fn();
  const { rerender } = render(renderRun(5, { onRerunStarted }));

  fireEvent.click(await screen.findByRole('button', { name: /re-run/i }));
  rerender(renderRun(7, { onRerunStarted }));
  assert.ok(await screen.findByText('task-7'));
  accepted.resolve(jsonResponse({ run_id: 9, status: 'running' }));
  await accepted.promise;

  fireEvent.click(screen.getByRole('button', { name: /re-run/i }));
  rerender(renderRun(8, { onRerunStarted }));
  assert.ok(await screen.findByText('task-8'));
  rejected.resolve(jsonResponse({ error: 'too late' }, 500));
  await rejected.promise;
  await new Promise((r) => setTimeout(r, 0));

  assert.equal(onRerunStarted.mock.calls.length, 0);
  assert.equal(screen.queryByText('too late') === null, true);
});

test('a failed load for the routed run shows its error', async () => {
  globalThis.fetch = vi.fn(async () => new Response('boom', { status: 500, statusText: 'Internal Server Error' }));
  render(renderRun(5));

  assert.ok(await screen.findByText('Get run failed: Internal Server Error'));
});

// A load for a run the view has left that fails late must not put its error
// on the run the user moved to.
test('a late failed load for the previous run is discarded', async () => {
  const oldLoad = deferred();
  globalThis.fetch = vi.fn(async (url) => (url.endsWith('/runs/5') ? oldLoad.promise : runsById(url)));
  const { rerender } = render(renderRun(5));

  rerender(renderRun(7));
  assert.ok(await screen.findByText('task-7'));
  oldLoad.resolve(new Response('boom', { status: 500, statusText: 'Internal Server Error' }));
  await oldLoad.promise;
  await new Promise((r) => setTimeout(r, 0));

  assert.equal(screen.queryByText(/Get run failed/) === null, true);
  assert.ok(screen.getByText('task-7'));
});
