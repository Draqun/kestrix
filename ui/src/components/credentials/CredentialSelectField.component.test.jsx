import assert from 'node:assert/strict';
import { useState } from 'react';
import { Button, Stack, TextField } from '@mui/material';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import { listCredentials } from '../../api/client.js';
import { remoteProjectPayload } from '../../lib/projectForm.js';
import CredentialSelectField from './CredentialSelectField.jsx';

let credentialItems;
let submitted;

beforeEach(() => {
  credentialItems = [
    { ref: 'cred_active', name: 'Existing active', type: 'token', revoked: false },
    { ref: 'cred_revoked', name: 'Revoked credential', type: 'token', revoked: true },
  ];
  submitted = null;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/credentials' && !options.method) {
      return new Response(JSON.stringify({ items: [...credentialItems] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/credentials' && options.method === 'POST') {
      const input = JSON.parse(options.body);
      const created = { ref: 'cred_new', name: input.name, type: input.type, revoked: false };
      credentialItems.push(created);
      return new Response(JSON.stringify(created), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
});

function ProjectDialogFields() {
  const [name, setName] = useState('demo');
  const [repoURL, setRepoURL] = useState('https://git.example.test/demo.git');
  const [repoRef, setRepoRef] = useState('release');
  const [credentialRef, setCredentialRef] = useState('');
  const [credentials, setCredentials] = useState(credentialItems);

  const refreshCredentials = async () => {
    const response = await listCredentials();
    setCredentials(response.items || []);
  };

  const submit = () => {
    submitted = remoteProjectPayload(name, {
      repo_url: repoURL,
      repo_ref: repoRef,
      repo_credential_ref: credentialRef,
      pipeline_dir: 'ci',
    });
  };

  return (
    <Stack spacing={2}>
      <TextField label="Project name" value={name} onChange={event => setName(event.target.value)} />
      <TextField label="Repository URL" value={repoURL} onChange={event => setRepoURL(event.target.value)} />
      <TextField label="Repository ref" value={repoRef} onChange={event => setRepoRef(event.target.value)} />
      <CredentialSelectField
        value={credentialRef}
        onChange={setCredentialRef}
        credentials={credentials}
        onCredentialsChanged={refreshCredentials}
      />
      <Button onClick={submit}>Create project</Button>
    </Stack>
  );
}

test('inline creation preserves project fields, selects metadata, and submits only its reference', async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <ProjectDialogFields />
    </MemoryRouter>,
  );

  const projectName = screen.getByLabelText('Project name');
  const repositoryURL = screen.getByLabelText('Repository URL');
  const repositoryRef = screen.getByLabelText('Repository ref');
  const credentialSelect = screen.getByRole('combobox', { name: 'Credential (optional)' });
  const createProject = screen.getByRole('button', { name: 'Create project' });
  await user.click(credentialSelect);
  await user.click(screen.getByRole('option', { name: /Existing active/ }));
  assert.match(credentialSelect.textContent, /Existing active/);
  await user.click(screen.getByRole('button', { name: 'Add credential' }));
  await user.type(screen.getByLabelText(/Display name/), 'New GitLab token');
  await user.type(screen.getByLabelText(/Token/), 'write-only-token');
  await user.click(screen.getByRole('button', { name: 'Create credential' }));

  await vi.waitFor(() => assert.match(credentialSelect.textContent, /New GitLab token/));
  assert.equal(projectName.value, 'demo');
  assert.equal(repositoryURL.value, 'https://git.example.test/demo.git');
  assert.equal(repositoryRef.value, 'release');

  await user.click(createProject);
  assert.equal(submitted.repo_credential_ref, 'cred_new');
  assert.equal('token' in submitted, false);
  assert.equal('password' in submitted, false);
  assert.equal('private_key' in submitted, false);
});
