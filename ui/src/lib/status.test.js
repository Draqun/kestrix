import { describe, it } from 'node:test';
import assert from 'node:assert';
import { formatStatus, getStatusColor, STATUS_INFO, isWaitingForManualApproval, isManualWaiting } from './status.js';

describe('status lib', () => {
  it('maps known statuses to colors and labels', () => {
    assert.deepStrictEqual(formatStatus('passed'), { color: 'success', label: 'PASSED' });
    assert.deepStrictEqual(formatStatus('failed'), { color: 'error', label: 'FAILED' });
    assert.deepStrictEqual(formatStatus('canceled'), { color: 'warning', label: 'CANCELED' });
    assert.deepStrictEqual(formatStatus('queued'), { color: 'info', label: 'QUEUED' });
    assert.deepStrictEqual(formatStatus('manual'), { color: 'info', label: 'MANUAL' });
    assert.deepStrictEqual(formatStatus('skipped'), { color: 'default', label: 'SKIPPED' });
    assert.deepStrictEqual(formatStatus('pending'), { color: 'default', label: 'PENDING' });
    assert.deepStrictEqual(formatStatus('running'), { color: 'warning', label: 'RUNNING' });
    assert.deepStrictEqual(formatStatus('unknown'), { color: 'warning', label: 'RUNNING' });
  });

  it('getStatusColor returns color for all statuses', () => {
    Object.keys(STATUS_INFO).forEach((status) => {
      assert.strictEqual(getStatusColor(status), STATUS_INFO[status].color);
    });
    assert.strictEqual(getStatusColor('other'), 'warning');
  });

  it('isWaitingForManualApproval returns true only for manual tasks in status manual without skip_reason', () => {
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual' }, { manual: true }), true);
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual' }, { Manual: true }), true);
    assert.strictEqual(isWaitingForManualApproval({ status: 'skipped' }, { manual: true }), false);
    assert.strictEqual(isWaitingForManualApproval({ status: 'skipped', error: 'secret missing' }, { manual: true }), false);
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual', skip_reason: { category: 'trigger_unsatisfied' } }, { manual: true }), false);
    assert.strictEqual(isWaitingForManualApproval({ status: 'skipped', skip_reason: { category: 'trigger_unsatisfied' } }, { manual: true }), false);
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual', manual: false }), false);
    assert.strictEqual(isWaitingForManualApproval(null, { manual: true }), false);
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual' }, null), false);
  });

  describe('isManualWaiting', () => {
    it('returns true when manual task has no dependencies', () => {
      const job = { name: 'deploy', status: 'manual' };
      const pipelineDef = { tasks: [{ name: 'deploy', manual: true, depends_on: [] }] };
      assert.strictEqual(isManualWaiting(job, pipelineDef, [job]), true);
    });

    it('returns false when task is not manual or not in status manual', () => {
      const pipelineDef = { tasks: [{ name: 'build', manual: false }] };
      assert.strictEqual(isManualWaiting({ name: 'build', status: 'manual' }, pipelineDef, []), false);
      assert.strictEqual(isManualWaiting({ name: 'deploy', status: 'passed' }, { tasks: [{ name: 'deploy', manual: true }] }, []), false);
      assert.strictEqual(isManualWaiting({ name: 'deploy', status: 'skipped' }, { tasks: [{ name: 'deploy', manual: true }] }, []), false);
    });

    it('returns false when manual task has skip_reason', () => {
      const job = { name: 'deploy', status: 'manual', skip_reason: { category: 'trigger_unsatisfied' } };
      const pipelineDef = { tasks: [{ name: 'deploy', manual: true }] };
      assert.strictEqual(isManualWaiting(job, pipelineDef, [job]), false);
    });

    it('returns true when dependency passed in current run', () => {
      const deployJob = { name: 'deploy', status: 'manual' };
      const buildJob = { name: 'build', status: 'passed' };
      const pipelineDef = {
        tasks: [
          { name: 'build' },
          { name: 'deploy', manual: true, depends_on: ['build'] },
        ],
      };
      assert.strictEqual(isManualWaiting(deployJob, pipelineDef, [buildJob, deployJob]), true);
    });

    it('returns false when dependency failed, canceled, or active in current run', () => {
      const deployJob = { name: 'deploy', status: 'manual' };
      const pipelineDef = {
        tasks: [
          { name: 'build' },
          { name: 'deploy', manual: true, depends_on: ['build'] },
        ],
      };
      for (const badStatus of ['failed', 'canceled', 'running', 'pending', 'claimed']) {
        const buildJob = { name: 'build', status: badStatus };
        assert.strictEqual(isManualWaiting(deployJob, pipelineDef, [buildJob, deployJob]), false);
      }
    });

    it('returns true when dependency was satisfied in previous executions (absent from current run)', () => {
      const deployJob = { name: 'deploy', status: 'manual' };
      const pipelineDef = {
        tasks: [
          { name: 'build' },
          { name: 'deploy', manual: true, depends_on: ['build'] },
        ],
      };
      // build is absent from current run (partial / single-task execution)
      assert.strictEqual(isManualWaiting(deployJob, pipelineDef, [deployJob]), true);
    });

    it('handles DAG custom nodes as input', () => {
      const deployNode = { id: 'task:deploy', data: { label: '⚙️ deploy', runStatus: 'manual' } };
      const buildNode = { id: 'task:build', data: { label: '⚙️ build', runStatus: 'passed' } };
      const pipelineDef = {
        Tasks: [
          { Name: 'build' },
          { Name: 'deploy', Manual: true, DependsOn: ['build'] },
        ],
      };
      assert.strictEqual(isManualWaiting(deployNode, pipelineDef, [buildNode, deployNode]), true);
    });
  });
});
