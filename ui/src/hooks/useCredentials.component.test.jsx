import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { beforeEach, test, vi } from 'vitest';
import useCredentials from './useCredentials.js';

function CredentialsProbe() {
  const { items, active, loading, error, refresh } = useCredentials();
  return (
    <div>
      <span data-testid="loading">{String(loading)}</span>
      <span data-testid="items">{items.length}</span>
      <span data-testid="active">{active.length}</span>
      <span data-testid="error">{error || ''}</span>
      <button onClick={refresh}>Refresh</button>
    </div>
  );
}

beforeEach(() => {
  globalThis.fetch = vi.fn();
});

test('credential hook exposes active metadata and handles a refresh failure', async () => {
  globalThis.fetch
    .mockResolvedValueOnce(new Response(JSON.stringify({
      items: [{ ref: 'active', revoked: false }, { ref: 'revoked', revoked: true }],
    }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: 'unavailable' }), { status: 503 }));

  render(<CredentialsProbe />);
  assert.equal((await screen.findByTestId('items')).textContent, '2');
  assert.equal(screen.getByTestId('active').textContent, '1');

  screen.getByRole('button', { name: 'Refresh' }).click();
  await vi.waitFor(() => assert.match(screen.getByTestId('error').textContent, /List credentials failed/));
  assert.equal(screen.getByTestId('loading').textContent, 'false');
});

test('credential hook normalizes a missing items collection', async () => {
  globalThis.fetch.mockResolvedValueOnce(new Response(JSON.stringify({}), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }));

  render(<CredentialsProbe />);
  await vi.waitFor(() => assert.equal(screen.getByTestId('loading').textContent, 'false'));
  assert.equal(screen.getByTestId('items').textContent, '0');
  assert.equal(screen.getByTestId('active').textContent, '0');
});
