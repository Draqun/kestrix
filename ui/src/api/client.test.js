import test from 'node:test';
import assert from 'node:assert/strict';
import {
  completeSetup, createAccount, createCredential, createProject, deleteAccount, getWebhookLogs,
  listAccounts, listCredentials, saveProjectPipeline, setAccountGrants, setAccountRole,
  login, logout, me, setUnauthorizedHandler, healthCheck, listProjects, setupStatus, runTask,
  listRunners, createRunner, revokeRunner,
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

test('every request carries the same-origin header and no Authorization header', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  const captured = [];
  globalThis.fetch = async (url, options = {}) => {
    captured.push({ url, headers: options.headers || {} });
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  await healthCheck();
  await listProjects();

  assert.equal(captured.length, 2);
  for (const call of captured) {
    assert.equal(call.headers['X-Kestrix-Request'], '1');
    assert.equal('Authorization' in call.headers, false);
  }
});

test('a POST request also carries the same-origin header', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = options;
    return new Response(JSON.stringify({}), { status: 200, headers: { 'Content-Type': 'application/json' } });
  };

  await createProject({ name: 'demo' });

  assert.equal(captured.method, 'POST');
  assert.equal(captured.headers['X-Kestrix-Request'], '1');
});

test('me returns the restored session', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  globalThis.fetch = async () => new Response(
    JSON.stringify({ name: 'alice', role: 'admin', expires_at: '2026-01-01T00:00:00Z' }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );

  const session = await me();
  assert.equal(session.name, 'alice');
  assert.equal(session.role, 'admin');
});

test('me throws on a 401 (no session), with .status attached', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  globalThis.fetch = async () => new Response('{}', { status: 401, headers: { 'Content-Type': 'application/json' } });

  await assert.rejects(() => me(), (err) => err.status === 401);
});

test('me throws with .status = 503 on a store outage, distinct from 401', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  globalThis.fetch = async () => new Response('{}', { status: 503, headers: { 'Content-Type': 'application/json' } });

  await assert.rejects(() => me(), (err) => err.status === 503);
});

test('logout posts to /api/auth/logout', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(null, { status: 204 });
  };

  await logout();

  assert.equal(captured.url, '/api/auth/logout');
  assert.equal(captured.options.method, 'POST');
});

test('logout always succeeds, even when the request itself fails', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  globalThis.fetch = async () => { throw new TypeError('Failed to fetch'); };

  // Must not throw - callers rely on this to unconditionally clear their
  // own local session state.
  await logout();
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
  t.after(() => { globalThis.fetch = originalFetch; });

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

test('listAccounts requests the users endpoint', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, method: options.method || 'GET' };
    return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'admin' }] }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  };

  const data = await listAccounts();

  assert.equal(captured.url, '/api/users');
  assert.equal(captured.method, 'GET');
  assert.deepEqual(data.items, [{ name: 'alice', role: 'admin' }]);
});

test('createAccount posts the name, password, and role', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(JSON.stringify({ name: 'bob', role: 'viewer' }), {
      status: 201, headers: { 'Content-Type': 'application/json' },
    });
  };

  const created = await createAccount({ name: 'bob', password: 's3cret', role: 'viewer' });

  assert.equal(captured.url, '/api/users');
  assert.equal(captured.options.method, 'POST');
  assert.deepEqual(JSON.parse(captured.options.body), { name: 'bob', password: 's3cret', role: 'viewer' });
  assert.equal(created.name, 'bob');
});

test('createAccount throws the server-provided error on a duplicate name', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'user already exists' }), {
    status: 409, headers: { 'Content-Type': 'application/json' },
  });

  await assert.rejects(
    createAccount({ name: 'bob', password: 's3cret', role: 'viewer' }),
    /user already exists/,
  );
});

test('setAccountRole puts the new role to the account role route', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(JSON.stringify({ name: 'bob', role: 'operator' }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  };

  await setAccountRole('bob', 'operator');

  assert.equal(captured.url, '/api/users/bob/role');
  assert.equal(captured.options.method, 'PUT');
  assert.deepEqual(JSON.parse(captured.options.body), { role: 'operator' });
});

test('setAccountGrants puts the full desired grant set to the account grants route', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(JSON.stringify({ name: 'bob', grants: [{ project_name: 'demo', role: 'operator' }] }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  };

  await setAccountGrants('bob', [{ project_name: 'demo', role: 'operator' }]);

  assert.equal(captured.url, '/api/users/bob/grants');
  assert.equal(captured.options.method, 'PUT');
  assert.deepEqual(JSON.parse(captured.options.body), { grants: [{ project_name: 'demo', role: 'operator' }] });
});

test('deleteAccount deletes the account route', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(null, { status: 204 });
  };

  await deleteAccount('bob');

  assert.equal(captured.url, '/api/users/bob');
  assert.equal(captured.options.method, 'DELETE');
});

test('deleteAccount throws the server-provided error on failure', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'user not found' }), {
    status: 404, headers: { 'Content-Type': 'application/json' },
  });

  await assert.rejects(deleteAccount('ghost'), /user not found/);
});

test('login throws the server-provided error message on failure', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });

  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'invalid name or password' }), {
    status: 401, headers: { 'Content-Type': 'application/json' },
  });

  await assert.rejects(() => login('alice', 'wrong'), /invalid name or password/);
});

test('setupStatus requests the setup endpoint', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, method: options.method || 'GET' };
    return new Response(JSON.stringify({ required: true }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  };

  const data = await setupStatus();

  assert.equal(captured.url, '/api/setup');
  assert.equal(captured.method, 'GET');
  assert.equal(data.required, true);
});

test('completeSetup posts the name and password to the setup endpoint', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(JSON.stringify({ name: 'admin', role: 'admin' }), {
      status: 201, headers: { 'Content-Type': 'application/json' },
    });
  };

  const created = await completeSetup('admin', 's3cret-pass');

  assert.equal(captured.url, '/api/setup');
  assert.equal(captured.options.method, 'POST');
  assert.deepEqual(JSON.parse(captured.options.body), { name: 'admin', password: 's3cret-pass' });
  assert.equal(created.name, 'admin');
});

test('completeSetup throws the server-provided error on 409', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'setup already completed' }), {
    status: 409, headers: { 'Content-Type': 'application/json' },
  });

  await assert.rejects(completeSetup('admin', 's3cret-pass'), /setup already completed/);
});

test('UI API starts a single-task re-run and returns the accepted run', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options) => {
    captured = { url, options };
    return new Response(
      JSON.stringify({ run_id: 42, project: 'my demo', task: 'build/x', status: 'queued' }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    );
  };

  const run = await runTask('my demo', 'build/x');

  assert.equal(captured.url, '/api/projects/my%20demo/tasks/build%2Fx/run');
  assert.equal(captured.options.method, 'POST');
  assert.deepEqual(run, { run_id: 42, project: 'my demo', task: 'build/x', status: 'queued' });
});

test('UI API surfaces a rejected single-task re-run', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(
    JSON.stringify({ error: 'task "gone" not found' }),
    { status: 404, headers: { 'Content-Type': 'application/json' } },
  );
  await assert.rejects(runTask('demo', 'gone'), /task "gone" not found/);

  globalThis.fetch = async () => new Response('not json', { status: 500, statusText: 'Internal Server Error' });
  await assert.rejects(runTask('demo', 'build'), /Re-run failed: Internal Server Error/);
});

test('listRunners requests the runners endpoint', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, method: options.method || 'GET' };
    return new Response(JSON.stringify({ items: [{ id: 1, name: 'gpu-box', tags: ['gpu'], status: 'online' }] }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    });
  };

  const data = await listRunners();

  assert.equal(captured.url, '/api/runners');
  assert.equal(captured.method, 'GET');
  assert.deepEqual(data.items.map((r) => r.name), ['gpu-box']);
});

test('listRunners throws when the list request fails', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response('{}', { status: 403, statusText: 'Forbidden' });

  await assert.rejects(listRunners(), /List runners failed: Forbidden/);
});

test('createRunner posts the name and tags and returns the one-time token', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, options };
    return new Response(JSON.stringify({ id: 2, name: 'gpu-box', tags: ['gpu'], token: 'tok' }), {
      status: 201, headers: { 'Content-Type': 'application/json' },
    });
  };

  const created = await createRunner({ name: 'gpu-box', tags: ['GPU'] });

  assert.equal(captured.url, '/api/runners');
  assert.equal(captured.options.method, 'POST');
  assert.deepEqual(JSON.parse(captured.options.body), { name: 'gpu-box', tags: ['GPU'] });
  assert.equal(created.token, 'tok');
});

test('createRunner surfaces the server error message', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'invalid runner tag "has space"' }), {
    status: 400, statusText: 'Bad Request', headers: { 'Content-Type': 'application/json' },
  });

  await assert.rejects(createRunner({ name: 'x', tags: ['has space'] }), /invalid runner tag "has space"/);
});

test('revokeRunner deletes the runner by id', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let captured;
  globalThis.fetch = async (url, options = {}) => {
    captured = { url, method: options.method };
    return new Response(null, { status: 204 });
  };

  await revokeRunner(3);

  assert.equal(captured.url, '/api/runners/3');
  assert.equal(captured.method, 'DELETE');
});

test('revokeRunner surfaces the server error message, falling back to the status text', async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async () => new Response(JSON.stringify({ error: 'runners not configured' }), {
    status: 500, statusText: 'Internal Server Error', headers: { 'Content-Type': 'application/json' },
  });
  await assert.rejects(revokeRunner(3), /runners not configured/);

  globalThis.fetch = async () => new Response('not json', { status: 502, statusText: 'Bad Gateway' });
  await assert.rejects(revokeRunner(3), /Revoke runner failed: Bad Gateway/);
});

