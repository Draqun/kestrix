import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { test } from 'vitest';
import NotFound from './NotFound.jsx';

test('not-found page says so and links back to the dashboard', () => {
  render(
    <MemoryRouter initialEntries={['/no/such/page']}>
      <NotFound />
    </MemoryRouter>,
  );

  assert.ok(screen.getByRole('heading', { name: 'Page not found' }));
  assert.equal(screen.getByRole('link', { name: 'Go to dashboard' }).getAttribute('href'), '/');
});
