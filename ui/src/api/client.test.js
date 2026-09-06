import test from 'node:test';
import assert from 'node:assert/strict';
import { createCredential, createProject, getWebhookLogs, listCredentials, saveProjectPipeline } from './client.js';
import { remoteProjectPayload } from '../lib/projectForm.js';

test('UI API creates a project with the remote-only payload', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options) => {
    captured = { url, options };
    return new Response(options.body, { status: 201, headers: { 'Content-Type': 'application/json' } });
  };

  const payload = remoteProjectPayload('demo', {
    repo_url: 'https://git.example.test/demo.git', repo_ref: 'main',
    repo_credential_ref: 'cred_demo', pipeline_dir: 'ci',
  });
  await createProject(payload);

  assert.equal(captured.url, '/api/projects');
  assert.equal(captured.options.method, 'POST');
  assert.deepEqual(JSON.parse(captured.options.body), payload);
  assert.equal('repo_path' in JSON.parse(captured.options.body), false);
});

test('UI API surfaces the explicit read-only pipeline response', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(
    JSON.stringify({ error: 'registered pipeline content is read-only; update it in the remote Git repository' }),
    { status: 405, headers: { 'Content-Type': 'application/json' } },
  );

  await assert.rejects(
    saveProjectPipeline('demo', 'project "demo" {}'),
    /registered pipeline content is read-only/,
  );
});

test('UI can create and refresh credential metadata before a project exists', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  const calls = [];
  globalThis.fetch = async (url, options = {}) => {
    calls.push({ url, options });
    if (options.method === 'POST') {
      return new Response(
        JSON.stringify({ ref: 'cred_new', name: 'GitLab', type: 'token', revoked: false }),
        { status: 201, headers: { 'Content-Type': 'application/json' } },
      );
    }
    return new Response(
      JSON.stringify({ items: [{ ref: 'cred_new', name: 'GitLab', type: 'token', revoked: false }] }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    );
  };

  const created = await createCredential({ name: 'GitLab', type: 'token', token: 'write-only' });
  const refreshed = await listCredentials();

  assert.equal(created.ref, 'cred_new');
  assert.deepEqual(refreshed.items, [created]);
  assert.deepEqual(calls.map(call => [call.url, call.options.method || 'GET']), [
    ['/api/credentials', 'POST'],
    ['/api/credentials', 'GET'],
  ]);
});

test('getWebhookLogs requests the filtered page and returns the envelope', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url) => {
    captured = url;
    return new Response(JSON.stringify({
      items: [{ id: 1, repo: 'org/web', status: 'error' }],
      pagination: { total: 1, limit: 10, offset: 0, next: null, prev: null },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  const data = await getWebhookLogs({
    limit: 10, offset: 20, project: 'web', status: 'error',
    since: '2026-09-01T10:00:00Z', until: '2026-09-01T11:00:00Z',
  });

  assert.match(captured, /^\/api\/logs\?/);
  for (const part of ['limit=10', 'offset=20', 'project=web', 'status=error', 'since=', 'until=']) {
    assert.ok(captured.includes(part), `${captured} missing ${part}`);
  }
  assert.equal(data.items.length, 1);
  assert.equal(data.items[0].repo, 'org/web');
  assert.equal(data.pagination.total, 1);
});

test('getWebhookLogs throws when the server rejects the request', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response('bad', { status: 400, statusText: 'Bad Request' });

  await assert.rejects(() => getWebhookLogs(), /Webhook history failed/);
});

test('getWebhookLogs tolerates a response without items', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response('{}', {
    status: 200, headers: { 'Content-Type': 'application/json' },
  });

  const data = await getWebhookLogs();
  assert.deepEqual(data.items, []);
  assert.equal(data.pagination.total, 0);
});
