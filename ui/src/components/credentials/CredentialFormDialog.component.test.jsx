import assert from 'node:assert/strict';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, test, vi } from 'vitest';
import CredentialFormDialog from './CredentialFormDialog.jsx';

let requests;
let createError;

function renderDialog(props = {}) {
  return render(
    <CredentialFormDialog
      open
      onClose={vi.fn()}
      onSuccess={vi.fn()}
      {...props}
    />,
  );
}

async function chooseType(user, label) {
  await user.click(screen.getByRole('combobox', { name: 'Type' }));
  await user.click(await screen.findByRole('option', { name: label }));
}

describe('CredentialFormDialog', () => {
  beforeEach(() => {
    requests = [];
    createError = '';
    globalThis.fetch = vi.fn(async (url, options = {}) => {
      requests.push({ url, options });
      if (createError) {
        return new Response(JSON.stringify({ error: createError }), {
          status: 400,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(JSON.stringify({ ref: 'cred_new', name: 'New credential', type: 'token', revoked: false }), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      });
    });
  });

  test('selects username/password with a pointer and shows only matching fields', async () => {
    const user = userEvent.setup();
    renderDialog();

    await chooseType(user, 'Username / app password');

    assert.ok(screen.getByLabelText(/Username/));
    assert.ok(screen.getByLabelText(/Password/));
    assert.equal(screen.queryByLabelText(/Token/), null);
    assert.equal(screen.queryByLabelText(/Private key/), null);
  });

  test('selects SSH with the keyboard', async () => {
    const user = userEvent.setup();
    renderDialog();

    const type = screen.getByRole('combobox', { name: 'Type' });
    await user.click(type);
    await user.keyboard('{End}{Enter}');

    assert.ok(await screen.findByLabelText(/Private key/));
    assert.ok(screen.getByLabelText(/Known hosts/));
    assert.equal(screen.queryByLabelText(/Token/), null);
  });

  test('clears incompatible secrets and submits only the selected type fields', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/Display name/), 'Bitbucket');
    await user.type(screen.getByLabelText(/Token/), 'must-not-leak');
    await chooseType(user, 'Username / app password');
    await user.type(screen.getByLabelText(/Username/), 'git-user');
    await user.type(screen.getByLabelText(/Password/), 'app-password');
    await user.click(screen.getByRole('button', { name: 'Create credential' }));

    await waitFor(() => assert.equal(requests.length, 1));
    const payload = JSON.parse(requests[0].options.body);
    assert.equal(payload.type, 'basic');
    assert.equal(payload.username, 'git-user');
    assert.equal(payload.password, 'app-password');
    assert.equal('token' in payload, false);
    assert.equal('private_key' in payload, false);
    assert.equal('known_hosts' in payload, false);
  });

  test('does not echo a submitted secret when creation fails', async () => {
    createError = 'Credential validation failed';
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/Display name/), 'GitLab');
    await user.type(screen.getByLabelText(/Token/), 'top-secret-token');
    await user.click(screen.getByRole('button', { name: 'Create credential' }));

    const alert = await screen.findByRole('alert');
    assert.match(alert.textContent, /Credential validation failed/);
    assert.doesNotMatch(alert.textContent, /top-secret-token/);
  });
});
