import { describe, it } from 'node:test';
import assert from 'node:assert';
import { formatStatus, getStatusColor, STATUS_INFO, isWaitingForManualApproval } from './status.js';

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
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual' }, { manual: false }), false);
    assert.strictEqual(isWaitingForManualApproval(null, { manual: true }), false);
    assert.strictEqual(isWaitingForManualApproval({ status: 'manual' }, null), false);
  });
});
