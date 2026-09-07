import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, test, vi } from 'vitest';
import Layout from './Layout.jsx';

const colorMode = vi.hoisted(() => ({ toggle: vi.fn() }));
vi.mock('../context/ColorModeContext.jsx', () => ({
  useColorMode: () => ({ mode: 'light', toggleColorMode: colorMode.toggle }),
}));
vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ name: 'test-admin', role: 'admin', logout: vi.fn(), roleAtLeast: () => true }),
}));
vi.mock('../hooks/useKeyboardNav.js', () => ({ default: () => {} }));

function matchesWidth(query, width) {
  const min = query.match(/min-width:\s*([\d.]+)px/);
  const max = query.match(/max-width:\s*([\d.]+)px/);
  return (!min || width >= Number(min[1])) && (!max || width <= Number(max[1]));
}

function setViewport(width) {
  window.innerWidth = width;
  window.matchMedia = vi.fn().mockImplementation((query) => ({
    matches: matchesWidth(query, width),
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent() { return false; },
  }));
}

function PathProbe() {
  return <div data-testid="path">{useLocation().pathname}</div>;
}

function renderLayout(width, path = '/') {
  setViewport(width);
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Layout><PathProbe /></Layout>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  colorMode.toggle.mockClear();
  setViewport(1200);
  globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ version: 'test' }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }));
});

test('desktop Settings navigation opens /settings and receives active state', async () => {
  const user = userEvent.setup();
  renderLayout(1200);

  const settings = screen.getByText('Settings');
  await user.click(settings);

  assert.equal(screen.getByTestId('path').textContent, '/settings');
  assert.equal(settings.closest('li')?.getAttribute('aria-current'), 'page');
  assert.equal(screen.queryByText('Repository credentials'), null);

  await user.click(screen.getByRole('button', { name: 'Switch to dark mode' }));
  assert.equal(colorMode.toggle.mock.calls.length, 1);
  await user.click(screen.getByRole('button', { name: 'Toggle navigation drawer' }));
});

test('mobile navigation keeps Settings reachable at 375px', async () => {
  const user = userEvent.setup();
  renderLayout(375);

  await user.click(screen.getByRole('button', { name: 'Settings' }));

  assert.equal(screen.getByTestId('path').textContent, '/settings');
  assert.equal(document.documentElement.scrollWidth <= 375, true);
});

test('tablet navigation recognizes a direct /settings route as active', () => {
  renderLayout(800, '/settings');

  const settings = screen.getByText('Settings');
  assert.equal(screen.getByTestId('path').textContent, '/settings');
  assert.equal(settings.closest('li')?.getAttribute('aria-current'), 'page');
});
