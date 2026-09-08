import assert from 'node:assert/strict';
import { test } from 'vitest';
import {
  changeCredentialType,
  credentialFieldDescriptors,
  credentialFormDefaults,
  credentialPayload,
  normalizeCredentialType,
} from './credentialForm.js';

test('credential helpers normalize empty metadata and all payload types', () => {
  assert.equal(normalizeCredentialType('basic'), 'basic');
  assert.equal(normalizeCredentialType('unknown'), 'token');
  assert.deepEqual(credentialFormDefaults(null), {
    name: '', type: 'token', provider: '', host: '', username: '', password: '',
    token: '', private_key: '', known_hosts: '', value: '',
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

test('credential descriptors define visible and supplemental guidance for every field', () => {
  const token = credentialFieldDescriptors('token');
  assert.deepEqual(token.map(field => field.key), [
    'name', 'type', 'provider', 'host', 'username', 'token',
  ]);
  assert.deepEqual(
    Object.fromEntries(token.map(field => [field.key, field.required])),
    { name: true, type: true, provider: false, host: false, username: false, token: true },
  );
  for (const descriptor of token) {
    assert.match(descriptor.helperText, /^(Required|Optional)\./);
    assert.ok(descriptor.tooltip.length > descriptor.helperText.length);
  }

  assert.deepEqual(
    credentialFieldDescriptors('basic').map(field => field.key),
    ['name', 'type', 'provider', 'host', 'username', 'password'],
  );
  assert.deepEqual(
    credentialFieldDescriptors('ssh').map(field => field.key),
    ['name', 'type', 'provider', 'host', 'private_key', 'known_hosts'],
  );
  assert.deepEqual(
    credentialFieldDescriptors('secret').map(field => field.key),
    ['name', 'type', 'provider', 'host', 'value'],
  );
  const rotation = credentialFieldDescriptors('ssh', { editing: true });
  assert.equal(rotation.find(field => field.key === 'private_key').label, 'Replacement private key');
  assert.match(rotation.find(field => field.key === 'known_hosts').helperText, /recommended/i);
  assert.match(rotation.find(field => field.key === 'known_hosts').tooltip, /accept-new|trust on first use/i);
  assert.match(token.find(field => field.key === 'provider').tooltip, /searchable metadata only/i);
  assert.match(token.find(field => field.key === 'host').tooltip, /does not validate.*restrict/i);
});

test('token payload includes an optional HTTPS username without leaking other type fields', () => {
  const payload = credentialPayload({
    name: 'GitHub', type: 'token', username: 'x-access-token', token: 'secret',
    password: 'wrong', private_key: 'wrong', known_hosts: 'wrong',
  });
  assert.deepEqual(payload, {
    name: 'GitHub', type: 'token', provider: '', host: '',
    username: 'x-access-token', token: 'secret',
  });
  assert.equal('password' in payload, false);
  assert.equal('private_key' in payload, false);
  assert.equal('known_hosts' in payload, false);
});
