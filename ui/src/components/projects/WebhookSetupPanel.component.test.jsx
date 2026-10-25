import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, test, vi } from 'vitest';
import WebhookSetupPanel from './WebhookSetupPanel.jsx';

const realOrigin = window.location.origin;

function withOrigin(origin) {
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { ...window.location, origin },
  });
}

afterEach(() => withOrigin(realOrigin));

test('the panel shows the delivery URL for the origin the browser is using', () => {
  withOrigin('https://ci.example.com');
  render(<WebhookSetupPanel hasProjectSecret />);
  assert.equal(screen.getByTestId('webhook-delivery-url').textContent, 'https://ci.example.com/webhook');
});

test('a reverse-proxied origin is reflected verbatim and called out', () => {
  withOrigin('https://kestrix.internal.example.net:8443');
  render(<WebhookSetupPanel hasProjectSecret />);
  assert.equal(
    screen.getByTestId('webhook-delivery-url').textContent,
    'https://kestrix.internal.example.net:8443/webhook',
  );
  assert.ok(screen.getByText(/Git host must be able to reach that same host/));
});

test('copying places exactly the delivery URL on the clipboard', async () => {
  withOrigin('https://ci.example.com');
  // userEvent.setup() installs its own clipboard stub, so the spy has to go in
  // after it or the click writes to that stub instead.
  const user = userEvent.setup();
  const writeText = vi.fn(async () => {});
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });

  render(<WebhookSetupPanel hasProjectSecret />);
  await user.click(screen.getByRole('button', { name: 'Copy to clipboard' }));

  assert.deepEqual(writeText.mock.calls, [['https://ci.example.com/webhook']]);
});

test('the panel names both accepted header forms and the GitLab steps', () => {
  render(<WebhookSetupPanel hasProjectSecret />);
  assert.ok(screen.getByText(/X-Gitlab-Token/));
  assert.ok(screen.getAllByText(/X-Hub-Signature-256/).length >= 1);
  assert.ok(screen.getAllByText(/Settings → Webhooks/).length >= 1);
  assert.ok(screen.getByText(/Secret token/));
  assert.ok(screen.getByText(/Push events/));
});

test('the panel shows GitHub steps and states deliveries are always verified by X-Hub-Signature-256', () => {
  render(<WebhookSetupPanel hasProjectSecret />);
  assert.ok(screen.getByText(/In GitHub/));
  assert.ok(screen.getByText(/Payload URL/));
  assert.ok(screen.getByText(/application\/json/));
  assert.ok(screen.getByText(/Just the push event/));
  assert.ok(screen.getByText(/GitHub deliveries are always verified by/));
  assert.ok(screen.getAllByText(/X-Hub-Signature-256/).length >= 2);
});

test('a project without its own secret is warned that the global secret is in use', () => {
  render(<WebhookSetupPanel hasProjectSecret={false} />);
  assert.ok(screen.getByText(/verified against the instance-wide/));

  render(<WebhookSetupPanel hasProjectSecret />);
  assert.equal(screen.queryAllByText(/verified against the instance-wide/).length, 1);
});
