import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { beforeEach, test, vi } from 'vitest';
import RepositoryCredentialsSection from './RepositoryCredentialsSection.jsx';

// TestRepositoryCredentialsSection secret row pins runtime-secrets tasks.md
// 6.4: the Settings credential list renders a `secret` row like any other
// credential, with its type label and rotate/revoke actions.
beforeEach(() => {
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url === '/api/credentials' && !options.method) {
      return new Response(JSON.stringify({
        items: [{
          ref: 'cred_secret',
          name: 'Deploy secret',
          type: 'secret',
          revoked: false,
        }],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
});

test('renders a secret credential row with its type label and rotate/revoke actions', async () => {
  render(<RepositoryCredentialsSection />);

  assert.ok(await screen.findByText('Deploy secret'));
  assert.match(screen.getByText(/secret ·/).textContent, /secret · generic · any host · cred_secret/);
  const actions = screen.getByTestId('credential-actions-cred_secret');
  assert.ok(actions.querySelector('button'));
  assert.ok(screen.getByRole('button', { name: 'Rotate' }));
  assert.ok(screen.getByRole('button', { name: 'Revoke' }));
});
