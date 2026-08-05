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

function assertGuidedField(label, required) {
  const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const control = screen.getByLabelText(new RegExp(`^${escaped}(?:\\s*\\*)?$`));
  const helperID = control.getAttribute('aria-describedby');
  assert.ok(helperID, `${label} should reference visible guidance`);
  const helper = document.getElementById(helperID.split(' ')[0]);
  assert.ok(helper, `${label} guidance should be rendered`);
  assert.match(helper.textContent, required ? /^Required\./ : /^Optional\./);
  assert.equal(
    control.required || control.getAttribute('aria-required') === 'true',
    required,
    `${label} required state should be explicit`,
  );
  assert.ok(screen.getByRole('button', {
    name: `Show guidance for ${label.replace(' (optional)', '').replace('Replacement ', '').toLowerCase()}`,
  }));
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

  test('guides every common and token field with requiredness and accessible tooltips', async () => {
    const user = userEvent.setup();
    renderDialog();

    assertGuidedField('Display name', true);
    assertGuidedField('Type', true);
    assertGuidedField('Provider (optional)', false);
    assertGuidedField('Host (optional)', false);
    assertGuidedField('HTTPS username (optional)', false);
    assertGuidedField('Token', true);

    const providerInfo = screen.getByRole('button', { name: 'Show guidance for provider' });
    await user.hover(providerInfo);
    assert.match((await screen.findByRole('tooltip')).textContent, /metadata only/i);
  });

  test('keeps guidance and required replacement fields in rotate mode', () => {
    renderDialog({
      credential: {
        ref: 'cred_gitlab', name: 'GitLab source', type: 'token',
        provider: 'gitlab', host: 'gitlab.com',
      },
    });

    assert.ok(screen.getByRole('heading', { name: 'Rotate GitLab source' }));
    assertGuidedField('Display name', true);
    assertGuidedField('Type', true);
    assertGuidedField('Provider (optional)', false);
    assertGuidedField('Host (optional)', false);
    assertGuidedField('HTTPS username (optional)', false);
    assertGuidedField('Replacement token', true);
    assert.ok(screen.getByRole('button', { name: 'Show guidance for token' }));
  });

  test('selects username/password with a pointer and shows only matching fields', async () => {
    const user = userEvent.setup();
    renderDialog();

    await chooseType(user, 'Username / app password');

    assert.ok(screen.getByLabelText(/Username/));
    assert.ok(screen.getByLabelText(/Password/));
    assert.equal(screen.queryByLabelText(/Token/), null);
    assert.equal(screen.queryByLabelText(/Private key/), null);

    assertGuidedField('Username', true);
    assertGuidedField('Password', true);
    await user.hover(screen.getByRole('button', { name: 'Show guidance for password' }));
    assert.match((await screen.findByRole('tooltip')).textContent, /app password/i);
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

    assertGuidedField('Private key', true);
    assertGuidedField('Known hosts (optional)', false);
    screen.getByRole('button', { name: 'Show guidance for known hosts' }).focus();
    assert.match((await screen.findByRole('tooltip')).textContent, /accept-new|trust on first use/i);
  });

  test('submits the optional HTTPS username with a token and no unrelated fields', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/Display name/), 'GitHub source');
    await user.type(screen.getByLabelText(/HTTPS username/), 'x-access-token');
    await user.type(screen.getByLabelText(/^Token/), 'github-token');
    await user.click(screen.getByRole('button', { name: 'Create credential' }));

    await waitFor(() => assert.equal(requests.length, 1));
    const payload = JSON.parse(requests[0].options.body);
    assert.equal(payload.type, 'token');
    assert.equal(payload.username, 'x-access-token');
    assert.equal(payload.token, 'github-token');
    assert.equal('password' in payload, false);
    assert.equal('private_key' in payload, false);
    assert.equal('known_hosts' in payload, false);
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
    createError = 'Credential validation failed for top-secret-token';
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText(/Display name/), 'GitLab');
    await user.type(screen.getByLabelText(/Token/), 'top-secret-token');
    await user.click(screen.getByRole('button', { name: 'Create credential' }));

    const alert = (await screen.findByText(/Credential validation failed/)).closest('[role="alert"]');
    assert.ok(alert);
    assert.match(alert.textContent, /Credential validation failed/);
    assert.doesNotMatch(alert.textContent, /top-secret-token/);
  });

  test('keeps source-only guidance and actions usable at 375px', () => {
    window.innerWidth = 375;
    window.matchMedia = vi.fn().mockImplementation(query => ({
      matches: query.includes('max-width:599.95px'),
      media: query,
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent() { return false; },
    }));

    renderDialog();

    assert.ok(screen.getByText(/encrypted and write-only/i));
    assert.ok(screen.getByText(/Git source.*only/i));
    assert.ok(screen.getByText(/deployment secrets.*separately/i));
    assertGuidedField('Display name', true);
    assertGuidedField('Type', true);
    assertGuidedField('Provider (optional)', false);
    assertGuidedField('Host (optional)', false);
    assertGuidedField('HTTPS username (optional)', false);
    assertGuidedField('Token', true);
    assert.ok(screen.getByRole('button', { name: 'Cancel' }));
    assert.ok(screen.getByRole('button', { name: 'Create credential' }));
    assert.equal(document.documentElement.scrollWidth <= 375, true);
  });
});
