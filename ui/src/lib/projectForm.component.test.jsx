import assert from 'node:assert/strict';
import { test } from 'vitest';
import {
  activeCredentialMetadata,
  createdCredentialSelection,
  remoteProjectPayload,
  repositorySourceLabel,
  validateRemoteProject,
} from './projectForm.js';

test('remote project helpers cover create, edit, validation, and source labels', () => {
  assert.deepEqual(remoteProjectPayload(' demo ', {
    repo_url: ' https://git.example.test/demo.git ',
    repo_ref: ' main ',
    repo_credential_ref: 'cred_git',
    pipeline_dir: ' ci ',
  }), {
    name: 'demo',
    repo_url: 'https://git.example.test/demo.git',
    repo_ref: 'main',
    repo_credential_ref: 'cred_git',
    pipeline_dir: 'ci',
    pipeline_file: 'pipeline.tf',
  });
  assert.deepEqual(remoteProjectPayload(undefined, {
    repo_url: undefined,
    repo_ref: undefined,
    repo_credential_ref: undefined,
    pipeline_dir: undefined,
    pipeline_file: 'custom.tf',
  }), {
    repo_url: '',
    repo_ref: '',
    repo_credential_ref: '',
    pipeline_dir: 'ci',
    pipeline_file: 'custom.tf',
  });

  assert.equal(validateRemoteProject('', {}), 'Project name is required');
  assert.equal(validateRemoteProject('demo', { repo_ref: 'main', pipeline_dir: 'ci' }), 'Repository URL is required');
  assert.equal(validateRemoteProject('demo', { repo_url: 'https://git.test/x', pipeline_dir: 'ci' }), 'Repository ref is required');
  assert.equal(validateRemoteProject('demo', { repo_url: 'https://git.test/x', repo_ref: 'main' }), 'Pipeline directory is required');
  assert.equal(validateRemoteProject(undefined, {
    repo_url: 'https://git.test/x', repo_ref: 'main', pipeline_dir: 'ci',
  }), '');

  const active = { ref: 'active', revoked: false };
  assert.deepEqual(activeCredentialMetadata([active, { ref: 'revoked', revoked: true }]), [active]);
  assert.deepEqual(activeCredentialMetadata(undefined), []);
  assert.equal(createdCredentialSelection('old', { ref: 'new' }), 'new');
  assert.equal(createdCredentialSelection('old', null), 'old');
  assert.equal(repositorySourceLabel({ Name: 'app', Path: 'src/app' }), 'app: src/app');
  assert.equal(
    repositorySourceLabel({ Name: 'docs', URL: 'https://git.test/docs.git', Ref: 'v1' }),
    'docs: https://git.test/docs.git @ v1',
  );
});
