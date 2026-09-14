import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { test, vi } from 'vitest';
import useAccounts from './useAccounts.js';

function AccountsProbe() {
  const { items, refresh } = useAccounts();
  return (
    <div>
      <span data-testid="items">{items.map((i) => i.name).join(',')}</span>
      <button onClick={refresh}>Refresh</button>
    </div>
  );
}

test('a stale in-flight refresh cannot overwrite a later, newer one', async () => {
  let resolveFirst;
  let resolveSecond;
  const first = new Promise((resolve) => { resolveFirst = resolve; });
  const second = new Promise((resolve) => { resolveSecond = resolve; });
  let call = 0;
  globalThis.fetch = vi.fn(async () => {
    call++;
    return call === 1 ? first : second;
  });

  render(<AccountsProbe />);
  // Fire a second, overlapping refresh before the mount-triggered first one resolves.
  screen.getByRole('button', { name: 'Refresh' }).click();

  resolveSecond(new Response(JSON.stringify({ items: [{ name: 'second' }] }), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  }));
  await vi.waitFor(() => assert.equal(screen.getByTestId('items').textContent, 'second'));

  // The stale first request now resolves - it must not clobber "second".
  resolveFirst(new Response(JSON.stringify({ items: [{ name: 'first' }] }), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  }));
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(screen.getByTestId('items').textContent, 'second');
});
