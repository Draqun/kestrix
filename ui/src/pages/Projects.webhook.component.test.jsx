import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import Projects from './Projects.jsx';

vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ name: 'test-admin', role: 'admin', logout: () => {}, roleAtLeast: () => true }),
}));

// The API never returns a stored secret; this is the value the server holds and
// which must therefore never appear anywhere in the rendered page.
const STORED_SECRET = 'stored-secret-must-never-render';

let requests;
let projectConfig;

function stubAPI() {
  requests = [];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET';
    requests.push({ url, method, body: options.body ? JSON.parse(options.body) : undefined });
    let body;
    if (url.startsWith('/api/projects?')) body = { items: [], pagination: null };
    else if (url === '/api/credentials') body = { items: [] };
    else if (url === '/api/projects/demo/definition') body = { name: 'demo', repos: [], tasks: [] };
    else if (url === '/api/projects/demo') body = projectConfig;
    else if (url === '/api/projects' && method === 'POST') body = { name: 'demo' };
    else throw new Error(`unexpected request: ${method} ${url}`);
    return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
}

beforeEach(() => {
  projectConfig = {
    name: 'demo',
    repo_url: 'https://git.example.test/demo.git',
    repo_ref: 'main',
    pipeline_dir: 'ci',
    pipeline_file: 'pipeline.tf',
    has_webhook_secret: true,
    sources: [],
  };
  stubAPI();
});

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/projects/demo']}>
      <Projects selectedName="demo" onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );
}

test('creating a project sends the typed webhook secret and pipeline file', async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );
  await screen.findByText(/No projects yet/);

  await user.click(screen.getByRole('button', { name: 'Add project' }));
  await user.type(screen.getByRole('textbox', { name: /Project name/ }), 'demo');
  await user.type(screen.getByRole('textbox', { name: /Repository URL/ }), 'https://git.example.test/demo.git');
  await user.clear(screen.getByRole('textbox', { name: /Pipeline file/ }));
  await user.type(screen.getByRole('textbox', { name: /Pipeline file/ }), 'ci.hcl');
  await user.type(screen.getByLabelText(/Webhook secret/), 'brand-new-secret');
  await user.click(screen.getByRole('button', { name: 'Create project' }));

  const created = requests.find(r => r.method === 'POST' && r.url === '/api/projects');
  assert.ok(created, 'create request was not sent');
  assert.equal(created.body.webhook_secret, 'brand-new-secret');
  assert.equal(created.body.pipeline_file, 'ci.hcl');
});

test('editing without touching the secret field omits webhook_secret entirely', async () => {
  const user = userEvent.setup();
  renderDetail();
  await screen.findByRole('heading', { name: 'demo' });

  await user.click(screen.getByRole('button', { name: 'Edit project' }));
  const ref = screen.getByRole('textbox', { name: /Repository ref/ });
  await user.clear(ref);
  await user.type(ref, 'v2.0.0');
  await user.click(screen.getByRole('button', { name: 'Save' }));

  const update = requests.find(r => r.method === 'PUT');
  assert.ok(update, 'update request was not sent');
  assert.equal(update.body.repo_ref, 'v2.0.0');
  assert.ok(!('webhook_secret' in update.body), 'an untouched secret field must not be sent');
});

test('typing in the edit dialog replaces the stored secret', async () => {
  const user = userEvent.setup();
  renderDetail();
  await screen.findByRole('heading', { name: 'demo' });

  await user.click(screen.getByRole('button', { name: 'Edit project' }));
  await user.type(screen.getByLabelText(/Webhook secret/), 'rotated-secret');
  await user.click(screen.getByRole('button', { name: 'Save' }));

  const update = requests.find(r => r.method === 'PUT');
  assert.ok(update, 'update request was not sent');
  assert.equal(update.body.webhook_secret, 'rotated-secret');
});

test('the edit dialog starts empty and no stored secret ever reaches the DOM', async () => {
  const user = userEvent.setup();
  // Even if the API regressed and leaked the secret, the UI must not render it.
  projectConfig.webhook_secret = STORED_SECRET;
  renderDetail();
  await screen.findByRole('heading', { name: 'demo' });

  assert.ok(!document.body.textContent.includes(STORED_SECRET));
  await user.click(screen.getByRole('button', { name: 'Edit project' }));

  assert.equal(screen.getByLabelText(/Webhook secret/).value, '');
  assert.ok(!document.body.textContent.includes(STORED_SECRET));
  assert.equal(document.body.querySelectorAll(`[value="${STORED_SECRET}"]`).length, 0);
});

test('the secret field can be revealed while typing and hides again', async () => {
  const user = userEvent.setup();
  renderDetail();
  await screen.findByRole('heading', { name: 'demo' });
  await user.click(screen.getByRole('button', { name: 'Edit project' }));

  const field = screen.getByLabelText(/Webhook secret/);
  assert.equal(field.type, 'password');
  await user.click(screen.getByRole('button', { name: 'Show webhook secret' }));
  assert.equal(field.type, 'text');
  await user.click(screen.getByRole('button', { name: 'Hide webhook secret' }));
  assert.equal(field.type, 'password');
});

test('the detail header reports a stored secret', async () => {
  renderDetail();
  const chip = await screen.findByTestId('webhook-secret-chip');
  assert.equal(within(chip).getByText('Secret set').textContent, 'Secret set');
});

test('a project without a secret is flagged as relying on the global secret', async () => {
  projectConfig.has_webhook_secret = false;
  renderDetail();
  const chip = await screen.findByTestId('webhook-secret-chip');
  assert.ok(chip.textContent.includes('No per-project secret'));
  assert.ok(screen.getByText(/verified against the instance-wide/));
});

test('the detail view carries the webhook setup panel', async () => {
  renderDetail();
  assert.ok(await screen.findByRole('heading', { name: 'Webhook setup' }));
  assert.ok(screen.getByTestId('webhook-delivery-url').textContent.endsWith('/webhook'));
});

test('the detail view shows a non-default pipeline file', async () => {
  projectConfig.pipeline_file = 'ci.hcl';
  renderDetail();
  await screen.findByRole('heading', { name: 'demo' });
  assert.ok(screen.getByText(/ci \/ ci\.hcl/));
});

test('cancelling the create dialog with a typed secret does not leak it into the next project', async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );
  await screen.findByText(/No projects yet/);

  await user.click(screen.getByRole('button', { name: 'Add project' }));
  await user.type(screen.getByRole('textbox', { name: /Project name/ }), 'project-a');
  await user.type(screen.getByLabelText(/Webhook secret/), 'project-a-secret');
  await user.click(screen.getByRole('button', { name: 'Cancel' }));

  // The dialog's exit transition can outlast the test, so this asserts on the
  // controlled values React already reset synchronously in the click handler,
  // rather than waiting for the Dialog to unmount before reopening it.
  assert.equal(screen.getByRole('textbox', { name: /Project name/ }).value, '');
  assert.equal(screen.getByLabelText(/Webhook secret/).value, '');
});

test('dismissing the create dialog without saving clears a typed secret', async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );
  await screen.findByText(/No projects yet/);

  await user.click(screen.getByRole('button', { name: 'Add project' }));
  await user.type(screen.getByLabelText(/Webhook secret/), 'leftover-secret');
  await user.keyboard('{Escape}');

  assert.equal(screen.getByLabelText(/Webhook secret/).value, '');
});

test('a rejected create surfaces the error and keeps the dialog open with the typed values', async () => {
  const user = userEvent.setup();
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET';
    if (url.startsWith('/api/projects?')) {
      return new Response(JSON.stringify({ items: [], pagination: null }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/credentials') {
      return new Response(JSON.stringify({ items: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/api/projects' && method === 'POST') {
      return new Response(JSON.stringify({ error: 'webhook_secret must be at least 8 characters' }), { status: 400, headers: { 'Content-Type': 'application/json' } });
    }
    throw new Error(`unexpected request: ${method} ${url}`);
  });

  render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );
  await screen.findByText(/No projects yet/);

  await user.click(screen.getByRole('button', { name: 'Add project' }));
  await user.type(screen.getByRole('textbox', { name: /Project name/ }), 'demo');
  await user.type(screen.getByRole('textbox', { name: /Repository URL/ }), 'https://git.example.test/demo.git');
  await user.type(screen.getByLabelText(/Webhook secret/), 'short');
  await user.click(screen.getByRole('button', { name: 'Create project' }));

  assert.ok(await screen.findByText('webhook_secret must be at least 8 characters'));
  assert.ok(screen.getByRole('dialog', { name: 'New project' }));
  assert.equal(screen.getByLabelText(/Webhook secret/).value, 'short');
});

test('a rejected update surfaces the error and keeps the dialog open', async () => {
  const user = userEvent.setup();
  requests = [];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET';
    requests.push({ url, method });
    if (url.startsWith('/api/projects?')) return new Response(JSON.stringify({ items: [], pagination: null }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    if (url === '/api/credentials') return new Response(JSON.stringify({ items: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    if (url === '/api/projects/demo/definition') return new Response(JSON.stringify({ name: 'demo', repos: [], tasks: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    if (url === '/api/projects/demo' && method === 'GET') return new Response(JSON.stringify(projectConfig), { status: 200, headers: { 'Content-Type': 'application/json' } });
    if (url === '/api/projects/demo' && method === 'PUT') {
      return new Response(JSON.stringify({ error: 'webhook_secret must be at least 8 characters' }), { status: 400, headers: { 'Content-Type': 'application/json' } });
    }
    throw new Error(`unexpected request: ${method} ${url}`);
  });

  renderDetail();
  await screen.findByRole('heading', { name: 'demo' });

  await user.click(screen.getByRole('button', { name: 'Edit project' }));
  await user.type(screen.getByLabelText(/Webhook secret/), 'short');
  await user.click(screen.getByRole('button', { name: 'Save' }));

  assert.ok(await screen.findByText('webhook_secret must be at least 8 characters'));
  assert.ok(screen.getByRole('dialog', { name: 'Edit project' }));
  assert.equal(screen.getByLabelText(/Webhook secret/).value, 'short');
});
