import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import { test } from 'vitest';
import DAGViewer, { isManualWaiting } from './DAGViewer.jsx';

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

test('isManualWaiting returns true when manual task has no dependencies', () => {
  const job = { name: 'deploy', status: 'manual' };
  const pipelineDef = { tasks: [{ name: 'deploy', manual: true, depends_on: [] }] };
  assert.equal(isManualWaiting(job, pipelineDef, [job]), true);
});

test('isManualWaiting returns false when task is not manual or not in status manual', () => {
  const pipelineDef = { tasks: [{ name: 'build', manual: false }] };
  assert.equal(isManualWaiting({ name: 'build', status: 'manual' }, pipelineDef, []), false);
  assert.equal(isManualWaiting({ name: 'deploy', status: 'passed' }, { tasks: [{ name: 'deploy', manual: true }] }, []), false);
  assert.equal(isManualWaiting({ name: 'deploy', status: 'skipped' }, { tasks: [{ name: 'deploy', manual: true }] }, []), false);
});

test('isManualWaiting returns false when manual task has skip_reason', () => {
  const job = { name: 'deploy', status: 'manual', skip_reason: { category: 'trigger_unsatisfied' } };
  const pipelineDef = { tasks: [{ name: 'deploy', manual: true }] };
  assert.equal(isManualWaiting(job, pipelineDef, [job]), false);
});

test('isManualWaiting returns true when dependency passed in current run', () => {
  const deployJob = { name: 'deploy', status: 'manual' };
  const buildJob = { name: 'build', status: 'passed' };
  const pipelineDef = {
    tasks: [
      { name: 'build' },
      { name: 'deploy', manual: true, depends_on: ['build'] },
    ],
  };
  assert.equal(isManualWaiting(deployJob, pipelineDef, [buildJob, deployJob]), true);
});

test('isManualWaiting returns false when dependency failed, canceled, or active in current run', () => {
  const deployJob = { name: 'deploy', status: 'manual' };
  const pipelineDef = {
    tasks: [
      { name: 'build' },
      { name: 'deploy', manual: true, depends_on: ['build'] },
    ],
  };
  for (const badStatus of ['failed', 'canceled', 'running', 'pending', 'claimed']) {
    const buildJob = { name: 'build', status: badStatus };
    assert.equal(isManualWaiting(deployJob, pipelineDef, [buildJob, deployJob]), false);
  }
});

test('isManualWaiting returns true when dependency was satisfied in previous executions (absent from current run)', () => {
  const deployJob = { name: 'deploy', status: 'manual' };
  const pipelineDef = {
    tasks: [
      { name: 'build' },
      { name: 'deploy', manual: true, depends_on: ['build'] },
    ],
  };
  // build is absent from current run (partial / single-task execution)
  assert.equal(isManualWaiting(deployJob, pipelineDef, [deployJob]), true);
});



test('isManualWaiting handles DAG custom nodes as input', () => {
  const deployNode = { id: 'task:deploy', data: { label: '⚙️ deploy', runStatus: 'manual' } };
  const buildNode = { id: 'task:build', data: { label: '⚙️ build', runStatus: 'passed' } };
  const pipelineDef = {
    Tasks: [
      { Name: 'build' },
      { Name: 'deploy', Manual: true, DependsOn: ['build'] },
    ],
  };
  assert.equal(isManualWaiting(deployNode, pipelineDef, [buildNode, deployNode]), true);
});
