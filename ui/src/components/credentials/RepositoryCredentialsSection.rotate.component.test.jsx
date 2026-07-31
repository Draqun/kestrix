import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, test, vi } from 'vitest';
import RepositoryCredentialsSection from './RepositoryCredentialsSection.jsx';

let listCalls;
let rotatePayload;

beforeEach(() => {
  listCalls = 0;
  rotatePayload = null;
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/credentials' && !options.method) {
      listCalls++;
      return new Response(JSON.stringify({
        items: [{
          ref: 'cred_gitlab',
          name: 'GitLab',
          type: 'token',
          provider: 'gitlab',
          host: 'gitlab.example',
          revoked: false,
        }],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/credentials/cred_gitlab' && options.method === 'PUT') {
      rotatePayload = JSON.parse(options.body);
      return new Response(JSON.stringify({
        ref: 'cred_gitlab',
        name: rotatePayload.name,
        type: rotatePayload.type,
        provider: rotatePayload.provider,
        host: rotatePayload.host,
        revoked: false,
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
});

test('rotating a credential submits a replacement secret and refreshes metadata', async () => {
  const user = userEvent.setup();
  render(<RepositoryCredentialsSection />);

  assert.ok(await screen.findByText('GitLab'));
  await user.click(screen.getByRole('button', { name: 'Rotate' }));
  await user.type(screen.getByLabelText(/Replacement token/), 'replacement-secret');
  await user.click(screen.getByRole('button', { name: 'Rotate credential' }));

  await vi.waitFor(() => assert.ok(rotatePayload));
  assert.equal(rotatePayload.type, 'token');
  assert.equal(rotatePayload.token, 'replacement-secret');
  assert.equal('password' in rotatePayload, false);
  assert.equal('private_key' in rotatePayload, false);
  await vi.waitFor(() => assert.equal(listCalls >= 2, true));
});
