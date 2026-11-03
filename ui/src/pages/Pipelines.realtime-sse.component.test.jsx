import assert from 'node:assert/strict';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, test, vi } from 'vitest';
import { RunDetail } from './Pipelines.jsx';

class MockEventSource {
  static instances = [];

  constructor(url, options) {
    this.url = url;
    this.options = options;
    this.listeners = {};
    this.readyState = 0;
    MockEventSource.instances.push(this);
  }

  addEventListener(event, listener) {
    if (!this.listeners[event]) this.listeners[event] = [];
    this.listeners[event].push(listener);
  }

  removeEventListener(event, listener) {
    if (!this.listeners[event]) return;
    this.listeners[event] = this.listeners[event].filter(l => l !== listener);
  }

  close() {
    this.readyState = 2;
    this.closed = true;
  }

  triggerOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  triggerError(err) {
    this.onerror?.(err);
  }

  emit(event, data) {
    const list = this.listeners[event] || [];
    const eventObj = { data: JSON.stringify(data) };
    for (const l of list) {
      l(eventObj);
    }
  }
}

const originalEventSource = globalThis.EventSource;

beforeEach(() => {
  MockEventSource.instances = [];
  globalThis.EventSource = MockEventSource;
  window.EventSource = MockEventSource;
});

afterEach(() => {
  globalThis.EventSource = originalEventSource;
  window.EventSource = originalEventSource;
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

test('real-time SSE stream appends log output and updates task status', async () => {
  // onOpen resyncs from the REST snapshot on every open (REVIEW.md R5), so
  // the mock must evolve like a real backend would - one that already
  // persisted each event before broadcasting it - or a resync landing after
  // a live SSE update would regress the UI back to a stale snapshot.
  let serverRun = {
    id: 10,
    project_name: 'test-proj',
    status: 'running',
    output: 'Initial output\n',
    task_results: [
      { name: 'build', status: 'running', output: '' },
    ],
    pipeline_def: {
      tasks: [{ name: 'build', execute: { script: 'echo hi' } }],
    },
  };
  let fetchCount = 0;
  globalThis.fetch = vi.fn(async () => {
    fetchCount++;
    return jsonResponse(serverRun);
  });

  render(
    <MemoryRouter>
      <RunDetail projectName="test-proj" runId={10} onBack={() => {}} />
    </MemoryRouter>,
  );

  // Initial load completes
  const runningChips = await screen.findAllByText('RUNNING');
  assert.ok(runningChips.length > 0);
  await vi.waitFor(() => assert.equal(MockEventSource.instances.length, 1));
  const es = MockEventSource.instances[0];

  act(() => {
    es.triggerOpen();
  });
  // onOpen resyncs once from the REST snapshot (REVIEW.md R5) - let that
  // settle, and its setDetail commit, before applying further live SSE
  // updates below; fetchCount alone only proves the call started, so flush
  // the macrotask queue afterward too.
  await vi.waitFor(() => assert.equal(fetchCount, 2));
  await new Promise((resolve) => setTimeout(resolve, 0));

  // Emit live trace event, tagged with its task_name as the real server
  // does post-REVIEW.md-R6 (TracePayload.TaskName)
  act(() => {
    es.emit('trace', { task_name: 'build', chunk: 'streaming live logs!\n', offset: 15 });
  });
  serverRun = {
    ...serverRun,
    output: serverRun.output + 'streaming live logs!\n',
    task_results: [{ name: 'build', status: 'running', output: 'streaming live logs!\n' }],
  };

  assert.ok(await screen.findByText(/streaming live logs!/));

  // Emit task completion
  act(() => {
    es.emit('task', { task_name: 'build', status: 'passed' });
  });
  serverRun = {
    ...serverRun,
    task_results: [{ ...serverRun.task_results[0], status: 'passed' }],
  };

  // REVIEW.md R18: assert the "build" job's own status chip, not just that
  // "PASSED" text appears somewhere - the run-level status from the
  // complete event alone could also produce that text.
  const buildRow = (await screen.findByText('build')).closest('li');
  assert.ok(buildRow, 'expected a job list row for "build"');
  assert.ok(within(buildRow).getByText('PASSED'), 'expected the build job row to show PASSED');

  // REVIEW.md R6/R18: the task_results entry for "build" must have been
  // updated in place, not replaced by a duplicate running/passed pair that
  // loses the live-streamed output - selecting the job must still show the
  // trace chunk that arrived while it was running.
  act(() => {
    fireEvent.click(within(buildRow).getByText('build'));
  });
  assert.ok(await screen.findByText(/streaming live logs!/));

  // Emit complete event
  serverRun = { ...serverRun, status: 'passed' };
  act(() => {
    es.emit('complete', { run_id: 10, status: 'passed' });
  });

  const passedChips = await screen.findAllByText('PASSED');
  assert.ok(passedChips.length > 0);
});

test('transparent fallback to polling when EventSource drops', async () => {
  let fetchCount = 0;
  globalThis.fetch = vi.fn(async () => {
    fetchCount++;
    return jsonResponse({
      id: 11,
      project_name: 'test-proj',
      status: 'running',
      output: 'Initial\n',
      task_results: [{ name: 'test', status: 'running' }],
      pipeline_def: {
        tasks: [{ name: 'test', execute: { script: 'make test' } }],
      },
    });
  });

  render(
    <MemoryRouter>
      <RunDetail projectName="test-proj" runId={11} onBack={() => {}} />
    </MemoryRouter>,
  );

  await vi.waitFor(() => assert.equal(fetchCount, 1));
  await vi.waitFor(() => assert.equal(MockEventSource.instances.length, 1));
  const es = MockEventSource.instances[0];

  // SSE opens - onOpen resyncs once from the REST snapshot (see REVIEW.md R5)
  act(() => {
    es.triggerOpen();
  });
  await vi.waitFor(() => assert.equal(fetchCount, 2));

  // Switch to fake timers to control polling intervals precisely
  vi.useFakeTimers();

  // Advance time while SSE is open: polling should NOT occur because SSE is active
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  assert.equal(fetchCount, 2);

  // Now SSE encounters an error (drop/disconnect)
  act(() => {
    es.triggerError(new Error('connection dropped'));
  });

  // Now that fallback is active, advance timer 1.5s: polling SHOULD occur
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1600);
  });
  assert.equal(fetchCount, 3);

  await act(async () => {
    await vi.advanceTimersByTimeAsync(1600);
  });
  assert.equal(fetchCount, 4);
});
