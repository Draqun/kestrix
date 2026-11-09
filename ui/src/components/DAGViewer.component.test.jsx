import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { test } from 'vitest';
import DAGViewer from './DAGViewer.jsx';

test('DAGViewer renders custom nodes with labels and statuses', () => {
  const nodes = [
    {
      id: 'repo:repo1',
      position: { x: 0, y: 0 },
      data: { label: '📦 repo1', type: 'repo' },
      type: 'custom',
    },
    {
      id: 'task:build',
      position: { x: 100, y: 0 },
      data: { label: '⚙️ build', type: 'task', runStatus: 'passed' },
      type: 'custom',
    },
    {
      id: 'task:deploy',
      position: { x: 200, y: 0 },
      data: { label: '⚙️ deploy', type: 'task', manual: true, runStatus: 'manual' },
      type: 'custom',
    },
  ];
  const edges = [
    { id: 'e1', source: 'repo:repo1', target: 'task:build' },
    { id: 'e2', source: 'task:build', target: 'task:deploy' },
  ];

  render(<DAGViewer nodes={nodes} edges={edges} />);
  assert.ok(screen.getByText('⚙️ deploy'));
  assert.ok(screen.getByText('⚙️ build'));
  assert.ok(screen.getByText('PASSED'));
  assert.ok(screen.getByText('MANUAL'));
});
