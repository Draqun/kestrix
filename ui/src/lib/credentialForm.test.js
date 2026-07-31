import test from 'node:test';
import assert from 'node:assert/strict';
import {
  changeCredentialType,
  credentialFormDefaults,
  credentialPayload,
  normalizeCredentialType,
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
});
