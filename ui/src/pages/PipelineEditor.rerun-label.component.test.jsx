import assert from 'node:assert/strict';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { test, vi } from 'vitest';
import PipelineEditor from './PipelineEditor.jsx';

// Only the run history is under test; the graph, code editor and layout
// libraries are heavy and irrelevant, so they are stubbed. The state setters
// must be stable references: the component's effects depend on them.
const stub = vi.hoisted(() => {
  const noop = () => {};
  return { flowState: [[], noop, noop], runs: { items: [] } };
});
vi.mock('@uiw/react-codemirror', () => ({ default: () => null }));
vi.mock('@codemirror/theme-one-dark', () => ({ oneDark: {} }));
vi.mock('reactflow', () => ({
  useNodesState: () => stub.flowState,
  useEdgesState: () => stub.flowState,
}));
vi.mock('../components/DAGViewer', () => ({ default: () => null }));
vi.mock('../lib/dagLayout', () => ({ layoutFromProject: () => ({ nodes: [], edges: [] }) }));
vi.mock('../api/client', () => ({
  getProjectPipeline: async () => '',
  getProjectDefinition: async () => ({ Repos: [], Tasks: [] }),
  resolveProject: async () => ({}),
  runProject: async () => ({}),
  listProjects: async () => ({ items: [] }),
  listRuns: async () => stub.runs,
}));

const base = {
  project_name: 'demo', status: 'passed', started_at: '2026-01-01T00:00:00Z', output: 'ok',
};

async function renderWithRuns(items) {
  stub.runs = { items };
  render(<PipelineEditor projectName="demo" onProjectChange={() => {}} />);
}

test('a single-task re-run is labelled with its task in the history and the detail', async () => {
  await renderWithRuns([{ ...base, id: 7, changed: [], rerun_task: 'build' }]);

  const entry = await screen.findByText('Re-run: build');
  await userEvent.click(entry);

  // the history entry and the detail dialog's chip both carry the label
  assert.equal((await screen.findAllByText('Re-run: build')).length, 2);
  assert.equal(screen.queryByText(/Changed:/) === null, true);
});

test('a whole run keeps its changed repositories in the history and the detail', async () => {
  await renderWithRuns([{ ...base, id: 8, changed: ['app'], rerun_task: '' }]);

  const entry = await screen.findByText('app');
  await userEvent.click(entry);

  assert.ok(await screen.findByText('Changed: app'));
  assert.equal(screen.queryByText(/Re-run:/) === null, true);
});
