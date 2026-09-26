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
        mode: 'remote',
        sources: [
          { name: 'demo', url: 'https://git.example.test/demo.git', primary: true },
          { name: 'shared', url: 'https://git.example.test/shared.git', primary: false },
        ],
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

  assert.ok((await screen.findAllByText('https://git.example.test/demo.git')).length > 0);
  assert.ok(await screen.findByText(/Credential: GitLab/));
  assert.equal(screen.queryByRole('heading', { name: 'Repository credentials' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Rotate' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Revoke' }), null);
});

test('project detail lists derived sources and marks the primary', async () => {
  render(
    <MemoryRouter initialEntries={['/projects/demo']}>
      <Projects selectedName="demo" onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText('Sources (2)'));
  const primary = await screen.findByTestId('project-source-https://git.example.test/demo.git');
  const shared = screen.getByTestId('project-source-https://git.example.test/shared.git');
  assert.ok(primary.textContent.includes('https://git.example.test/demo.git'));
  assert.ok(primary.textContent.includes('Primary'));
  assert.ok(shared.textContent.includes('https://git.example.test/shared.git'));
  assert.ok(!shared.textContent.includes('Primary'));
});

test('project detail renders the sources derived by the definition load', async () => {
  // The definition route derives sources server-side, so the config read must
  // happen after it: before this ordering the page rendered the pre-refresh
  // set and never re-read it.
  let definitionFetched = false;
  const requests = [];
  globalThis.fetch = vi.fn(async (url) => {
    requests.push(url);
    let body;
    if (url.startsWith('/api/projects?')) {
      body = { items: [], pagination: null };
    } else if (url === '/api/credentials') {
      body = { items: [] };
    } else if (url === '/api/projects/demo/definition') {
      definitionFetched = true;
      body = { name: 'demo', repos: [], tasks: [] };
    } else if (url === '/api/projects/demo') {
      body = {
        name: 'demo',
        repo_url: 'https://git.example.test/demo.git',
        repo_ref: 'main',
        pipeline_dir: 'ci',
        // Sources exist only once the definition load has derived them.
        sources: definitionFetched
          ? [
              { name: 'demo', url: 'https://git.example.test/demo.git', primary: true },
              { name: 'shared', url: 'https://git.example.test/shared.git', primary: false },
            ]
          : [],
      };
    } else {
      throw new Error(`unexpected request: GET ${url}`);
    }
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });

  render(
    <MemoryRouter initialEntries={['/projects/demo']}>
      <Projects selectedName="demo" onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );

  assert.ok(await screen.findByText('Sources (2)'));
  assert.ok(screen.getByTestId('project-source-https://git.example.test/shared.git'));
  assert.equal(screen.queryByText('Derived on the next pipeline load.'), null);
  const definitionAt = requests.indexOf('/api/projects/demo/definition');
  const configAt = requests.indexOf('/api/projects/demo');
  assert.ok(definitionAt !== -1 && configAt !== -1);
  assert.ok(definitionAt < configAt, 'definition must be requested before the project config');
});

test('project detail shows the execution mode', async () => {
  render(
    <MemoryRouter initialEntries={['/projects/demo']}>
      <Projects selectedName="demo" onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );

  assert.equal((await screen.findByTestId('project-mode-chip')).textContent, 'Mode: remote');
});
