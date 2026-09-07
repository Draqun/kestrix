import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import Projects from './Projects.jsx';

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
    if (url.startsWith('/api/projects?')) {
      return new Response(JSON.stringify({
        items: [{ name: 'alpha', repo_url: 'https://git.example/alpha.git', repo_ref: 'main', pipeline_dir: 'ci' }],
        pagination: null,
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/credentials') {
      return new Response(JSON.stringify({ items: [] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });
});

function renderProjects() {
  return render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={() => {}} onOpenPipeline={() => {}} />
    </MemoryRouter>,
  );
}

test('viewer sees no project lifecycle actions', async () => {
  authState.role = 'viewer';
  renderProjects();

  assert.ok(await screen.findByText('alpha'));
  assert.equal(screen.queryByRole('button', { name: 'Add project' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Delete project' }), null);
});

test('admin sees project lifecycle actions', async () => {
  authState.role = 'admin';
  renderProjects();

  assert.ok(await screen.findByText('alpha'));
  assert.ok(screen.getByRole('button', { name: 'Add project' }));
  assert.ok(screen.getByRole('button', { name: 'Delete project' }));
});

// The UI hiding a button is a usability choice, not the enforcement boundary:
// access-control's spec requires the server to refuse the action regardless.
// That is asserted server-side by internal/web's route-role table test
// (TestRouteRoles_EnforceMinimumRole), not repeated here.
