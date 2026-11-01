import { describe, it } from 'node:test';
import assert from 'node:assert';
import { applyTaskResultsToDagNodes, layoutFromProject } from './dagLayout.js';

describe('dagLayout', () => {
  it('handles empty or missing project in layoutFromProject', () => {
    const res = layoutFromProject(null);
    assert.deepStrictEqual(res, { nodes: [], edges: [] });
  });

  it('applies task results to dag nodes', () => {
    const nodes = [
      { id: 'repo:main', data: { type: 'repo' } },
      { id: 'task:build', data: { type: 'task' } },
      { id: 'task:test', data: { type: 'task' } },
    ];
    const taskResults = [
      { name: 'build', status: 'passed' },
      { name: 'test', status: 'skipped', skip_reason: { category: 'trigger_unsatisfied' } },
    ];

    const updated = applyTaskResultsToDagNodes(nodes, taskResults);
    assert.strictEqual(updated[0].data.runStatus, undefined);
    assert.strictEqual(updated[1].data.runStatus, 'passed');
    assert.strictEqual(updated[1].data.skipCategory, undefined);
    assert.strictEqual(updated[2].data.runStatus, 'skipped');
    assert.strictEqual(updated[2].data.skipCategory, 'trigger_unsatisfied');
  });

  it('handles default empty task results', () => {
    const nodes = [{ id: 'task:build', data: { type: 'task' } }];
    const updated = applyTaskResultsToDagNodes(nodes);
    assert.strictEqual(updated[0].data.runStatus, undefined);
  });
});
