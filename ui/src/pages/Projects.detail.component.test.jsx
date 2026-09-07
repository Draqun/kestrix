import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import Projects from './Projects.jsx';

vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ name: 'test-admin', role: 'admin', logout: () => {}, roleAtLeast: () => true }),
}));

beforeEach(() => {
  globalThis.fetch = vi.fn(async (url) => {
    let body;
    if (url.startsWith('/api/projects?')) {
      body = { items: [], pagination: null };
    } else if (url === '/api/credentials') {
      body = { items: [{ ref: 'cred_active', name: 'GitLab', type: 'token', revoked: false }] };
    } else if (url === '/api/projects/demo/definition') {
      body = { name: 'demo', repos: [], tasks: [] };
    } else if (url === '/api/projects/demo') {
      body = {
        name: 'demo',
        repo_url: 'https://git.example.test/demo.git',
        repo_ref: 'main',
        repo_credential_ref: 'cred_active',
        pipeline_dir: 'ci',
      };
    } else {
      throw new Error(`unexpected request: GET ${url}`);
    }
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
});

test('project detail shows credential metadata without embedding lifecycle controls', async () => {
  render(
    <MemoryRouter initialEntries={['/projects/demo']}>
      <Projects selectedName="demo" onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText('https://git.example.test/demo.git'));
  assert.ok(await screen.findByText(/Credential: GitLab/));
  assert.equal(screen.queryByRole('heading', { name: 'Repository credentials' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Rotate' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Revoke' }), null);
});
