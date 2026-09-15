import test from 'node:test';
import assert from 'node:assert/strict';
import {
  activeCredentialMetadata,
  createdCredentialSelection,
  remoteProjectPayload,
  repositorySourceLabel,
  validateRemoteProject,
} from './projectForm.js';

test('remote project payload contains only the remote source contract', () => {
  const payload = remoteProjectPayload(' demo ', {
    repo_url: ' https://git.example.test/demo.git ', repo_ref: ' main ',
    repo_credential_ref: 'gitlab-build', pipeline_dir: ' ci ',
    repo_path: '/srv/demo', extra_repos: [{ name: 'ignored' }],
  });
  assert.deepEqual(payload, {
    name: 'demo', repo_url: 'https://git.example.test/demo.git', repo_ref: 'main',
    repo_credential_ref: 'gitlab-build', pipeline_dir: 'ci', pipeline_file: 'pipeline.tf',
  });
});

test('an empty credential reference explicitly selects public access', () => {
  const payload = remoteProjectPayload(undefined, {
    repo_url: 'https://git.example.test/demo.git', repo_ref: 'main',
    repo_credential_ref: '', pipeline_dir: 'ci',
  });
  assert.equal(payload.repo_credential_ref, '');
});

test('remote URL, ref, pipeline directory, and create name are required', () => {
  assert.equal(validateRemoteProject('', {}), 'Project name is required');
  assert.equal(validateRemoteProject('demo', { repo_ref: 'main', pipeline_dir: 'ci' }), 'Repository URL is required');
  assert.equal(validateRemoteProject('demo', { repo_url: 'https://git.test/x', pipeline_dir: 'ci' }), 'Repository ref is required');
  assert.equal(validateRemoteProject('demo', { repo_url: 'https://git.test/x', repo_ref: 'main' }), 'Pipeline directory is required');
  assert.equal(validateRemoteProject('demo', { repo_url: 'https://git.test/x', repo_ref: 'main', pipeline_dir: 'ci' }), '');
});

test('parsed repository labels distinguish subpaths from remotes', () => {
  assert.equal(repositorySourceLabel({ Name: 'app', Path: 'src/app' }), 'app: src/app');
  assert.equal(repositorySourceLabel({ Name: 'docs', URL: 'https://git.test/docs.git', Ref: 'v1' }), 'docs: https://git.test/docs.git @ v1');
});

test('project onboarding offers only active credential metadata', () => {
  const credentials = [
    { ref: 'cred_active', name: 'Active', revoked: false },
    { ref: 'cred_revoked', name: 'Revoked', revoked: true },
  ];
  assert.deepEqual(activeCredentialMetadata(credentials), [credentials[0]]);
  assert.deepEqual(activeCredentialMetadata(undefined), []);
});

test('a credential created during onboarding becomes the selected reference', () => {
  assert.equal(createdCredentialSelection('', { ref: 'cred_new', name: 'New' }), 'cred_new');
  assert.equal(createdCredentialSelection('cred_old', {}), 'cred_old');
});

test('a typed webhook secret reaches the create payload', () => {
  const payload = remoteProjectPayload('demo', {
    repo_url: 'https://git.example.test/demo.git', repo_ref: 'main',
    pipeline_dir: 'ci', webhook_secret: '  s3cr3t  ',
  });
  assert.equal(payload.webhook_secret, 's3cr3t');
});

test('an untouched webhook secret field is omitted so the stored secret survives an edit', () => {
  for (const webhook_secret of [undefined, '', '   ']) {
    const payload = remoteProjectPayload(undefined, {
      repo_url: 'https://git.example.test/demo.git', repo_ref: 'v2',
      pipeline_dir: 'ci', webhook_secret,
    });
    assert.ok(!('webhook_secret' in payload), `blank secret ${JSON.stringify(webhook_secret)} must not be sent`);
  }
});

test('a replacement webhook secret reaches the update payload', () => {
  const payload = remoteProjectPayload(undefined, {
    repo_url: 'https://git.example.test/demo.git', repo_ref: 'main',
    pipeline_dir: 'ci', webhook_secret: 'rotated',
  });
  assert.equal(payload.webhook_secret, 'rotated');
});

test('an edited pipeline file replaces the default', () => {
  const form = { repo_url: 'https://git.example.test/demo.git', repo_ref: 'main', pipeline_dir: 'ci' };
  assert.equal(remoteProjectPayload(undefined, { ...form, pipeline_file: ' ci.hcl ' }).pipeline_file, 'ci.hcl');
  assert.equal(remoteProjectPayload(undefined, { ...form, pipeline_file: '  ' }).pipeline_file, 'pipeline.tf');
  assert.equal(remoteProjectPayload(undefined, form).pipeline_file, 'pipeline.tf');
});
