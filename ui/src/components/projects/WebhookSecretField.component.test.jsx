import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import WebhookSecretField from './WebhookSecretField.jsx';

test('renders with password type by default and shows helper text', () => {
  render(
    <WebhookSecretField
      value="my-secret"
      onChange={vi.fn()}
      helperText="Optional secret for HMAC verification"
    />,
  );

  const input = screen.getByLabelText(/Webhook secret/);
  assert.equal(input.getAttribute('type'), 'password');
  assert.equal(input.value, 'my-secret');
  assert.ok(screen.getByText('Optional secret for HMAC verification'));
  assert.ok(screen.getByRole('button', { name: 'Show webhook secret' }));
});

test('toggles password visibility when show/hide button is clicked', async () => {
  const user = userEvent.setup();
  render(
    <WebhookSecretField
      value="my-secret"
      onChange={vi.fn()}
    />,
  );

  const input = screen.getByLabelText(/Webhook secret/);
  assert.equal(input.getAttribute('type'), 'password');

  const toggleButton = screen.getByRole('button', { name: 'Show webhook secret' });
  await user.click(toggleButton);

  assert.equal(input.getAttribute('type'), 'text');
  assert.ok(screen.getByRole('button', { name: 'Hide webhook secret' }));

  await user.click(screen.getByRole('button', { name: 'Hide webhook secret' }));
  assert.equal(input.getAttribute('type'), 'password');
  assert.ok(screen.getByRole('button', { name: 'Show webhook secret' }));
});

test('calls onChange when user types in the input', async () => {
  const user = userEvent.setup();
  const handleChange = vi.fn();
  render(
    <WebhookSecretField
      value=""
      onChange={handleChange}
    />,
  );

  const input = screen.getByLabelText(/Webhook secret/);
  await user.type(input, 'a');

  assert.equal(handleChange.mock.calls.length, 1);
  assert.equal(handleChange.mock.calls[0][0], 'a');
});
