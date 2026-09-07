import test from 'node:test';
import assert from 'node:assert/strict';
import {
  createCredential, createProject, getWebhookLogs, listCredentials, saveProjectPipeline,
  login, setAuthToken, setUnauthorizedHandler, healthCheck, listProjects,
} from './client.js';
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

test('every request attaches the bearer token once it is set', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; setAuthToken(null); });

  const captured = [];
  globalThis.fetch = async (url, options = {}) => {
    captured.push({ url, headers: options.headers || {} });
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  setAuthToken('test-token-123');
  await healthCheck();
  await listProjects();

  assert.equal(captured.length, 2);
  for (const call of captured) {
    assert.equal(call.headers.Authorization, 'Bearer test-token-123');
  }
});

test('no request attaches a token before one is set', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = options.headers || {};
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  setAuthToken(null);
  await healthCheck();

  assert.equal('Authorization' in captured, false);
});

test('a 401 response invokes the unauthorized handler exactly once', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; setUnauthorizedHandler(null); });

  globalThis.fetch = async () => new Response('{}', { status: 401, headers: { 'Content-Type': 'application/json' } });

  let calls = 0;
  setUnauthorizedHandler(() => { calls++; });
  await healthCheck();

  assert.equal(calls, 1);
});

test('a non-401 response never invokes the unauthorized handler', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; setUnauthorizedHandler(null); });

  globalThis.fetch = async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });

  let calls = 0;
  setUnauthorizedHandler(() => { calls++; });
  await healthCheck();

  assert.equal(calls, 0);
});

test('login posts credentials without an Authorization header and returns the token', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; setAuthToken(null); });

  setAuthToken('stale-token-from-a-previous-session');
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(JSON.stringify({ token: 'fresh-token', expires_at: '2026-01-01T00:00:00Z' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  };

  const resp = await login('alice', 's3cret');

  assert.equal(captured.url, '/api/auth/login');
  assert.deepEqual(JSON.parse(captured.options.body), { name: 'alice', password: 's3cret' });
  assert.equal('Authorization' in (captured.options.headers || {}), false);
  assert.equal(resp.token, 'fresh-token');
});

test('login throws the server-provided error message on failure', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'invalid name or password' }), {
    status: 401, headers: { 'Content-Type': 'application/json' },
  });

  await assert.rejects(() => login('alice', 'wrong'), /invalid name or password/);
});
