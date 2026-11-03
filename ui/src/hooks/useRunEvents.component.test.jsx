import assert from 'node:assert/strict';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, test, vi } from 'vitest';
import useRunEvents from './useRunEvents.js';

class MockEventSource {
  static instances = [];
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  // Test-controlled: when true, the constructor throws instead of
  // connecting, simulating a browser that rejects the URL/options.
  static throwOnConstruct = false;

  constructor(url, options) {
    if (MockEventSource.throwOnConstruct) {
      throw new Error('EventSource construction failed');
    }
    this.url = url;
    this.options = options;
    this.listeners = {};
    this.readyState = MockEventSource.CONNECTING;
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
    this.readyState = MockEventSource.CLOSED;
    this.closed = true;
  }

  triggerOpen() {
    this.readyState = MockEventSource.OPEN;
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

  // emitRaw bypasses JSON.stringify, for testing the malformed-payload path.
  emitRaw(event, rawData) {
    const list = this.listeners[event] || [];
    const eventObj = { data: rawData };
    for (const l of list) {
      l(eventObj);
    }
  }
}

function HookProbe({ projectName, runId, enabled = true, onStatus, onTask, onTrace, onComplete, onError }) {
  const { isConnected, isFallback } = useRunEvents({
    projectName,
    runId,
    enabled,
    onStatus,
    onTask,
    onTrace,
    onComplete,
    onError,
  });

  return (
    <div>
      <span data-testid="connected">{String(isConnected)}</span>
      <span data-testid="fallback">{String(isFallback)}</span>
    </div>
  );
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
});

test('useRunEvents connects to SSE and processes typed events', () => {
  const onStatus = vi.fn();
  const onTask = vi.fn();
  const onTrace = vi.fn();
  const onComplete = vi.fn();

  render(
    <HookProbe
      projectName="demo-proj"
      runId={10}
      onStatus={onStatus}
      onTask={onTask}
      onTrace={onTrace}
      onComplete={onComplete}
    />
  );

  assert.equal(MockEventSource.instances.length, 1);
  const es = MockEventSource.instances[0];
  assert.equal(es.url, '/api/projects/demo-proj/runs/10/events');
  assert.equal(es.options?.withCredentials, true);

  act(() => {
    es.triggerOpen();
  });

  assert.equal(screen.getByTestId('connected').textContent, 'true');
  assert.equal(screen.getByTestId('fallback').textContent, 'false');

  // Emit task
  act(() => {
    es.emit('task', { task_name: 'build', status: 'running' });
  });
  assert.equal(onTask.mock.calls.length, 1);
  assert.deepEqual(onTask.mock.calls[0][0], { task_name: 'build', status: 'running' });

  // Emit trace
  act(() => {
    es.emit('trace', { chunk: 'build started\n', offset: 0 });
  });
  assert.equal(onTrace.mock.calls.length, 1);
  assert.deepEqual(onTrace.mock.calls[0][0], { chunk: 'build started\n', offset: 0 });

  // Emit status
  act(() => {
    es.emit('status', { run_id: 10, status: 'passed' });
  });
  assert.equal(onStatus.mock.calls.length, 1);
  assert.deepEqual(onStatus.mock.calls[0][0], { run_id: 10, status: 'passed' });

  // Emit complete
  act(() => {
    es.emit('complete', { run_id: 10, status: 'passed' });
  });
  assert.equal(onComplete.mock.calls.length, 1);
  assert.deepEqual(onComplete.mock.calls[0][0], { run_id: 10, status: 'passed' });
  assert.equal(es.closed, true);
});

test('useRunEvents flags fallback on error or disconnect', () => {
  const onError = vi.fn();
  render(
    <HookProbe
      projectName="demo-proj"
      runId={10}
      onError={onError}
    />
  );

  const es = MockEventSource.instances[0];
  act(() => {
    es.triggerOpen();
  });
  assert.equal(screen.getByTestId('connected').textContent, 'true');

  act(() => {
    es.triggerError(new Error('Network error'));
  });

  assert.equal(screen.getByTestId('connected').textContent, 'false');
  assert.equal(screen.getByTestId('fallback').textContent, 'true');
  assert.equal(onError.mock.calls.length, 1);
});

test('useRunEvents flags fallback if EventSource is unsupported', () => {
  delete window.EventSource;
  delete globalThis.EventSource;

  render(
    <HookProbe
      projectName="demo-proj"
      runId={10}
    />
  );

  assert.equal(screen.getByTestId('connected').textContent, 'false');
  assert.equal(screen.getByTestId('fallback').textContent, 'true');
});

// REVIEW.md R19: an error before the connection ever opens (the stream
// failed to establish) must flag fallback the same as a drop after open.
test('useRunEvents flags fallback on an error before the connection opens', () => {
  const onError = vi.fn();
  render(<HookProbe projectName="demo-proj" runId={10} onError={onError} />);

  const es = MockEventSource.instances[0];
  assert.equal(screen.getByTestId('connected').textContent, 'false');

  act(() => {
    es.triggerError(new Error('failed to establish'));
  });

  assert.equal(screen.getByTestId('connected').textContent, 'false');
  assert.equal(screen.getByTestId('fallback').textContent, 'true');
  assert.equal(onError.mock.calls.length, 1);
});

// REVIEW.md R1/R19: onerror re-closes the connection when the browser has
// already marked it CLOSED, rather than leaving a half-torn-down stream.
test('useRunEvents closes the EventSource on error once readyState is CLOSED', () => {
  render(<HookProbe projectName="demo-proj" runId={10} />);

  const es = MockEventSource.instances[0];
  act(() => {
    es.triggerOpen();
  });
  es.readyState = MockEventSource.CLOSED;
  const closeSpy = vi.spyOn(es, 'close');

  act(() => {
    es.triggerError(new Error('connection closed'));
  });

  assert.equal(closeSpy.mock.calls.length, 1);
});

// REVIEW.md R19: becoming inactive (enabled -> false) must close the
// EventSource and reset isConnected, the same as unmounting does.
test('useRunEvents closes the EventSource when it becomes disabled', () => {
  const { rerender } = render(<HookProbe projectName="demo-proj" runId={10} enabled={true} />);

  const es = MockEventSource.instances[0];
  act(() => {
    es.triggerOpen();
  });
  assert.equal(screen.getByTestId('connected').textContent, 'true');

  rerender(<HookProbe projectName="demo-proj" runId={10} enabled={false} />);

  assert.equal(es.closed, true);
  assert.equal(screen.getByTestId('connected').textContent, 'false');
});

// REVIEW.md R19: a runId change must tear down the old connection and open
// a fresh one for the new run, not keep streaming the old run's events.
test('useRunEvents reconnects to a new EventSource when runId changes', () => {
  const { rerender } = render(<HookProbe projectName="demo-proj" runId={10} />);

  const firstEs = MockEventSource.instances[0];
  act(() => {
    firstEs.triggerOpen();
  });

  rerender(<HookProbe projectName="demo-proj" runId={11} />);

  assert.equal(firstEs.closed, true);
  assert.equal(MockEventSource.instances.length, 2);
  const secondEs = MockEventSource.instances[1];
  assert.equal(secondEs.url, '/api/projects/demo-proj/runs/11/events');
});

// REVIEW.md R19: unmounting must close the EventSource (the effect's own
// cleanup), not leave it connected after the component is gone.
test('useRunEvents closes the EventSource on unmount', () => {
  const { unmount } = render(<HookProbe projectName="demo-proj" runId={10} />);

  const es = MockEventSource.instances[0];
  act(() => {
    es.triggerOpen();
  });

  unmount();

  assert.equal(es.closed, true);
});

// REVIEW.md R17/R19: a malformed event payload must not throw or invoke
// the handler with garbage - it should be dropped, with the connection
// otherwise unaffected.
test('useRunEvents drops a malformed SSE payload without calling the handler', () => {
  const onTask = vi.fn();
  const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
  render(<HookProbe projectName="demo-proj" runId={10} onTask={onTask} />);

  const es = MockEventSource.instances[0];
  act(() => {
    es.triggerOpen();
  });

  act(() => {
    es.emitRaw('task', '{not valid json');
  });

  assert.equal(onTask.mock.calls.length, 0);
  assert.equal(consoleError.mock.calls.length, 1);
  consoleError.mockRestore();
});

// REVIEW.md R17: a callback firing on a stale EventSource after cleanup
// (active=false) - open, an event, or an error arriving after unmount or a
// runId change raced ahead of it - must be a no-op, not touch state for a
// component that is already gone.
test('useRunEvents ignores callbacks on a stale EventSource after cleanup', () => {
  const onTask = vi.fn();
  const onError = vi.fn();
  const { unmount } = render(<HookProbe projectName="demo-proj" runId={10} onTask={onTask} onError={onError} />);

  const es = MockEventSource.instances[0];
  unmount();

  act(() => {
    es.triggerOpen();
    es.emit('task', { task_name: 'build', status: 'running' });
    es.triggerError(new Error('late error'));
  });

  assert.equal(onTask.mock.calls.length, 0);
  assert.equal(onError.mock.calls.length, 0);
});

// REVIEW.md R17/R19: if the browser throws constructing EventSource itself
// (not just failing to connect), the hook must still fall back to polling.
test('useRunEvents flags fallback when the EventSource constructor throws', () => {
  const onError = vi.fn();
  MockEventSource.throwOnConstruct = true;
  try {
    render(<HookProbe projectName="demo-proj" runId={10} onError={onError} />);
  } finally {
    MockEventSource.throwOnConstruct = false;
  }

  assert.equal(screen.getByTestId('connected').textContent, 'false');
  assert.equal(screen.getByTestId('fallback').textContent, 'true');
  assert.equal(onError.mock.calls.length, 1);
});
