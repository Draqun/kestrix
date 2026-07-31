import assert from 'node:assert/strict';
import { test } from 'vitest';
import {
  changeCredentialType,
  credentialFormDefaults,
  credentialPayload,
  normalizeCredentialType,
} from './credentialForm.js';

test('credential helpers normalize empty metadata and all payload types', () => {
  assert.equal(normalizeCredentialType('basic'), 'basic');
  assert.equal(normalizeCredentialType('unknown'), 'token');
  assert.deepEqual(credentialFormDefaults(null), {
    name: '', type: 'token', provider: '', host: '', username: '', password: '',
    token: '', private_key: '', known_hosts: '',
  });

  const changed = changeCredentialType({ token: 'old', username: 'old', password: 'old' }, 'ssh');
  assert.equal(changed.type, 'ssh');
  assert.equal(changed.token, '');
  assert.equal(changed.username, '');
  assert.equal(changed.password, '');
  assert.equal(changed.private_key, '');
  assert.equal(changed.known_hosts, '');

  assert.deepEqual(credentialPayload({ type: 'unknown' }), {
    name: '', type: 'token', provider: '', host: '', token: '',
  });
  assert.deepEqual(credentialPayload({ type: 'basic' }), {
    name: '', type: 'basic', provider: '', host: '', username: '', password: '',
  });
  assert.deepEqual(credentialPayload({ type: 'ssh' }), {
    name: '', type: 'ssh', provider: '', host: '', private_key: '', known_hosts: '',
  });
});
