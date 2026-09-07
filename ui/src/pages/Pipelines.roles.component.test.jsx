import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import { RunList } from './Pipelines.jsx';

const authState = vi.hoisted(() => ({ role: 'viewer' }));
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
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/runs') {
      return new Response(JSON.stringify([]), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });
});

function renderRunList() {
  return render(
    <MemoryRouter initialEntries={['/pipelines']}>
      <RunList onNewRun={() => {}} onOpenRun={() => {}} />
    </MemoryRouter>,
  );
}

test('viewer sees no "New run" action', async () => {
  authState.role = 'viewer';
  renderRunList();

  assert.ok(await screen.findByText('Runs'));
  assert.equal(screen.queryByRole('button', { name: 'New run' }), null);
});

test('operator sees the "New run" action', async () => {
  authState.role = 'operator';
  renderRunList();

  assert.ok(await screen.findByText('Runs'));
  assert.ok(screen.getByRole('button', { name: 'New run' }));
});
