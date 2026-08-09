import assert from 'node:assert/strict';
import { fireEvent, render, screen } from '@testing-library/react';
import { test, vi } from 'vitest';
import useKeyboardNav from './useKeyboardNav.js';

function KeyboardHarness({ navigate }) {
  useKeyboardNav(navigate);
  return <input aria-label="Editable field" />;
}

test('g+s opens Settings while editable fields keep keyboard input', () => {
  const navigate = vi.fn();
  render(<KeyboardHarness navigate={navigate} />);

  fireEvent.keyDown(window, { key: 'g' });
  fireEvent.keyDown(window, { key: 's' });
  assert.deepEqual(navigate.mock.calls, [['/settings']]);

  const input = screen.getByLabelText('Editable field');
  fireEvent.keyDown(input, { key: 'g' });
  fireEvent.keyDown(input, { key: 's' });
  assert.deepEqual(navigate.mock.calls, [['/settings']]);

  fireEvent.keyDown(window, { key: 'g' });
  fireEvent.keyDown(window, { key: 'x' });
  fireEvent.keyDown(window, { key: 'g' });
  fireEvent.keyDown(window, { key: 's' });
  assert.deepEqual(navigate.mock.calls, [['/settings'], ['/settings']]);
});
