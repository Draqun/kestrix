import assert from 'node:assert/strict';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import Projects from './Projects.jsx';

vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ name: 'test-admin', role: 'admin', logout: () => {}, roleAtLeast: () => true }),
}));

beforeEach(() => {
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
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url.startsWith('/api/projects?')) {
      return new Response(JSON.stringify({ items: [], pagination: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/credentials' && !options.method) {
      return new Response(JSON.stringify({ items: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
});

test('project onboarding controls remain reachable at 375px', async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );

  await screen.findByText(/No projects yet/);
  await user.click(screen.getByRole('button', { name: 'Add project' }));

  const dialog = screen.getByRole('dialog', { name: 'New project' });
  assert.ok(within(dialog).getByLabelText(/Project name/));
  assert.ok(within(dialog).getByLabelText(/Repository URL/));
  assert.ok(within(dialog).getByRole('combobox', { name: 'Credential (optional)' }));
  assert.ok(within(dialog).getByRole('button', { name: 'Add credential' }));
  assert.ok(within(dialog).getByRole('link', { name: 'Manage' }));
  assert.equal(document.documentElement.scrollWidth <= 375, true);
});
