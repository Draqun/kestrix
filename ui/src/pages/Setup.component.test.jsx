import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import Setup from './Setup.jsx';

const auth = vi.hoisted(() => ({ completeSetup: vi.fn() }));
vi.mock('../context/AuthContext.jsx', () => ({
  useAuth: () => ({ completeSetup: auth.completeSetup }),
}));

test('submits name and password to completeSetup', async () => {
  auth.completeSetup.mockResolvedValue({ token: 'tok', expires_at: '2026-01-01T00:00:00Z' });
  const user = userEvent.setup();
  render(<Setup />);

  await user.type(screen.getByLabelText('Name'), 'admin');
  await user.type(screen.getByLabelText('Password'), 's3cret-pass');
  await user.type(screen.getByLabelText('Confirm password'), 's3cret-pass');
  await user.click(screen.getByRole('button', { name: 'Create administrator' }));

  assert.deepEqual(auth.completeSetup.mock.calls[0], ['admin', 's3cret-pass']);
});

test('disables submit until every field is filled', () => {
  render(<Setup />);
  assert.equal(screen.getByRole('button', { name: 'Create administrator' }).disabled, true);
});

test('rejects a submit when the passwords do not match, without calling completeSetup', async () => {
  auth.completeSetup.mockClear();
  const user = userEvent.setup();
  render(<Setup />);

  await user.type(screen.getByLabelText('Name'), 'admin');
  await user.type(screen.getByLabelText('Password'), 's3cret-pass');
  await user.type(screen.getByLabelText('Confirm password'), 'different');
  await user.click(screen.getByRole('button', { name: 'Create administrator' }));

  assert.ok(await screen.findByText('Passwords do not match'));
  assert.equal(auth.completeSetup.mock.calls.length, 0);
});

test('shows a 409 conflict as an explanatory message', async () => {
  const err = new Error('setup already completed');
  err.status = 409;
  auth.completeSetup.mockRejectedValue(err);
  const user = userEvent.setup();
  render(<Setup />);

  await user.type(screen.getByLabelText('Name'), 'admin');
  await user.type(screen.getByLabelText('Password'), 's3cret-pass');
  await user.type(screen.getByLabelText('Confirm password'), 's3cret-pass');
  await user.click(screen.getByRole('button', { name: 'Create administrator' }));

  assert.ok(await screen.findByText(/Setup was already completed/));
});

test('shows the server error on any other failure', async () => {
  auth.completeSetup.mockRejectedValue(new Error('name and password are required'));
  const user = userEvent.setup();
  render(<Setup />);

  await user.type(screen.getByLabelText('Name'), 'admin');
  await user.type(screen.getByLabelText('Password'), 's3cret-pass');
  await user.type(screen.getByLabelText('Confirm password'), 's3cret-pass');
  await user.click(screen.getByRole('button', { name: 'Create administrator' }));

  assert.ok(await screen.findByText('name and password are required'));
});
