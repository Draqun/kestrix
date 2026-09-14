import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import AccountsSection from './AccountsSection.jsx';

vi.mock('../../context/AuthContext.jsx', () => ({
  useAuth: () => ({ name: 'alice', roleAtLeast: () => true }),
}));

test('changing your own role asks a distinct, more explicit confirmation', async () => {
  globalThis.fetch = vi.fn(async (url) => {
    if (url === '/api/users') {
      return new Response(JSON.stringify({ items: [{ name: 'alice', role: 'admin', grants: [] }] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });
  let prompt = null;
  vi.stubGlobal('confirm', (message) => { prompt = message; return false; });

  const user = userEvent.setup();
  render(<AccountsSection />);

  const row = within(await screen.findByTestId('account-row-alice'));
  await user.click(row.getByLabelText('Role'));
  await user.click(await screen.findByRole('option', { name: 'viewer' }));

  assert.match(prompt, /your own global role/);
});
