import assert from 'node:assert/strict';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { test, vi } from 'vitest';
import App from './App.jsx';

// ui-single-task-rerun "Navigation after re-run": RunDetailRoutes wires
// onRerunStarted to the run detail route of the new run.

vi.mock('./context/AuthContext.jsx', () => ({
  AuthProvider: ({ children }) => children,
  useAuth: () => ({
    isAuthenticated: true,
    setupRequired: false,
    name: 'test-user',
    role: 'operator',
    login: vi.fn(),
    logout: vi.fn(),
    completeSetup: vi.fn(),
    roleAtLeast: () => true,
  }),
}));
vi.mock('./hooks/useKeyboardNav.js', () => ({ default: () => {} }));

function jsonResponse(body) {
  return new Response(JSON.stringify(body), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  });
}

test('an accepted re-run navigates to the new run under an encoded project name', async () => {
  window.history.pushState({}, '', '/pipelines/runs/my%20demo/5');
  const calls = [];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    calls.push({ url, method: options.method || 'GET' });
    if (options.method === 'POST' && url.includes('/tasks/')) {
      return jsonResponse({ run_id: 9, project: 'my demo', task: 'build', status: 'queued' });
    }
    const match = /\/runs\/(\d+)$/.exec(url);
    if (match) {
      const id = Number(match[1]);
      return jsonResponse({
        id, project_name: 'my demo', status: id === 5 ? 'failed' : 'queued',
        started_at: '2026-01-01T00:00:00Z', started_by: 'user:alice',
        task_results: id === 5 ? [{ name: 'build', status: 'failed' }] : [],
      });
    }
    return jsonResponse({});
  });

  render(<App />);
  // At jsdom's width Layout opens its modal navigation drawer, which marks
  // the page content aria-hidden, so the query must include hidden nodes.
  fireEvent.click(await screen.findByRole('button', { name: /re-run/i, hidden: true }));

  await waitFor(() => assert.equal(window.location.pathname, '/pipelines/runs/my%20demo/9'));
  assert.ok(await screen.findByText('Run #9'));
  assert.ok(calls.some(c => c.method === 'POST' && c.url === '/api/projects/my%20demo/tasks/build/run'));
});
