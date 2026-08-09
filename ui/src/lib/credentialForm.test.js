import test from 'node:test';
import assert from 'node:assert/strict';
import {
  changeCredentialType,
  credentialFieldDescriptors,
  credentialFormDefaults,
  credentialPayload,
  normalizeCredentialType,
  redactCredentialError,
} from './credentialForm.js';

test('credential form defaults normalize unsupported types and omit stored secrets', () => {
  assert.equal(normalizeCredentialType('basic'), 'basic');
  assert.equal(normalizeCredentialType('unsupported'), 'token');
  assert.deepEqual(credentialFormDefaults({
    name: 'GitLab', type: 'unsupported', provider: 'gitlab', host: 'gitlab.example', token: 'never-copy',
  }), {
    name: 'GitLab', type: 'token', provider: 'gitlab', host: 'gitlab.example',
    username: '', password: '', token: '', private_key: '', known_hosts: '',
  });
});

test('changing type clears every incompatible secret field', () => {
  const changed = changeCredentialType({
    ...credentialFormDefaults(), token: 'token', username: 'user', password: 'password',
    private_key: 'key', known_hosts: 'host',
  }, 'ssh');
  assert.equal(changed.type, 'ssh');
  assert.equal(changed.token, '');
  assert.equal(changed.username, '');
  assert.equal(changed.password, '');
  assert.equal(changed.private_key, '');
  assert.equal(changed.known_hosts, '');
});

test('credential payload includes common metadata and only selected type fields', () => {
  const common = {
    name: ' Bitbucket ', provider: ' bitbucket ', host: ' bitbucket.org ',
    username: 'git-user', password: 'app-password', token: 'old-token',
    private_key: 'old-key', known_hosts: 'old-hosts', type: 'basic',
  };
  assert.deepEqual(credentialPayload(common), {
    name: 'Bitbucket', provider: 'bitbucket', host: 'bitbucket.org', type: 'basic',
    username: 'git-user', password: 'app-password',
  });
  assert.deepEqual(credentialPayload({ ...common, type: 'ssh', private_key: 'key', known_hosts: 'hosts' }), {
    name: 'Bitbucket', provider: 'bitbucket', host: 'bitbucket.org', type: 'ssh',
    private_key: 'key', known_hosts: 'hosts',
  });
  assert.deepEqual(credentialPayload({ ...common, type: 'token', token: 'token' }), {
    name: 'Bitbucket', provider: 'bitbucket', host: 'bitbucket.org', type: 'token',
    username: 'git-user', token: 'token',
  });
});

test('field descriptors track type-specific requiredness and rotation labels', () => {
  const fields = credentialFieldDescriptors('basic', { editing: true });
  assert.deepEqual(fields.map(field => [field.key, field.required]), [
    ['name', true], ['type', true], ['provider', false], ['host', false],
    ['username', true], ['password', true],
  ]);
  assert.equal(fields.find(field => field.key === 'password').label, 'Replacement password');
  assert.ok(fields.every(field => field.helperText && field.tooltip));
});

test('credential errors redact every submitted secret value', () => {
  const form = {
    token: 'token-secret', password: 'password-secret', private_key: 'private-key-secret',
  };
  const message = redactCredentialError(
    'Rejected token-secret, password-secret, and private-key-secret',
    form,
  );
  assert.equal(message, 'Rejected [REDACTED], [REDACTED], and [REDACTED]');
});
