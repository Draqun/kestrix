import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import Projects from './Projects.jsx';

let credentialItems;
beforeEach(() => {
  credentialItems = [
    { ref: 'cred_active', name: 'Existing active', type: 'token', revoked: false },
    { ref: 'cred_revoked', name: 'Revoked credential', type: 'token', revoked: true },
  ];
  globalThis.fetch = vi.fn(async (url, options = {}) => {
    if (url.startsWith('/api/projects?')) {
      return new Response(JSON.stringify({ items: [], pagination: null }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/api/credentials' && !options.method) {
      return new Response(JSON.stringify({ items: [...credentialItems] }), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error(`unexpected request: ${options.method || 'GET'} ${url}`);
  });
});

function renderProjects() {
  return render(
    <MemoryRouter initialEntries={['/projects']}>
      <Projects selectedName={null} onSelect={vi.fn()} onOpenPipeline={vi.fn()} />
    </MemoryRouter>,
  );
}

test('project onboarding uses the compact active-credential selector without a manager', async () => {
  const user = userEvent.setup();
  renderProjects();
  await screen.findByText(/No projects yet/);

  assert.equal(screen.queryByRole('heading', { name: 'Repository credentials' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Rotate' }), null);
  assert.equal(screen.queryByRole('button', { name: 'Revoke' }), null);

  await user.click(screen.getByRole('button', { name: 'Add project' }));
  const credentialSelect = screen.getByRole('combobox', { name: 'Credential (optional)' });
  await user.click(credentialSelect);
  assert.ok(screen.getByRole('option', { name: /Existing active/ }));
  assert.equal(screen.queryByRole('option', { name: /Revoked credential/ }), null);
  await user.keyboard('{Escape}');
  assert.ok(screen.getByRole('button', { name: 'Add credential' }));
  assert.ok(screen.getByRole('link', { name: 'Manage' }));
});
