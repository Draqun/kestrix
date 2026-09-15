import { describe, it } from 'node:test';
import assert from 'node:assert';
import { filterRuns, getUniqueRepos, RUN_STATUSES } from './runFilters.js';

const runs = [
  { id: 1, status: 'passed', changed: ['auth'] },
  { id: 2, status: 'failed', changed: [] },
  { id: 3, status: 'running', changed: ['auth', 'frontend'] },
  { id: 4, status: 'canceled', changed: ['frontend'] },
  { id: 5, status: 'queued', changed: [] },
];

describe('filterRuns', () => {
  it('returns all runs when no filters are set', () => {
    const result = filterRuns(runs, { statusFilter: [], changeFilter: '' });
    assert.deepStrictEqual(result, runs);
  });

  it('filters by a single status', () => {
    const result = filterRuns(runs, { statusFilter: ['passed'], changeFilter: '' });
    assert.strictEqual(result.length, 1);
    assert.strictEqual(result[0].id, 1);
  });

  it('filters by multiple statuses', () => {
    const result = filterRuns(runs, { statusFilter: ['passed', 'running'], changeFilter: '' });
    assert.strictEqual(result.length, 2);
    assert.deepStrictEqual(result.map((r) => r.id), [1, 3]);
  });

  it('filters by the queued status', () => {
    const result = filterRuns(runs, { statusFilter: ['queued'], changeFilter: '' });
    assert.strictEqual(result.length, 1);
    assert.strictEqual(result[0].id, 5);
  });

  it('filters by changed repository', () => {
    const result = filterRuns(runs, { statusFilter: [], changeFilter: 'frontend' });
    assert.strictEqual(result.length, 2);
    assert.deepStrictEqual(result.map((r) => r.id), [3, 4]);
  });

  it('filters by status and changed repository combined', () => {
    const result = filterRuns(runs, { statusFilter: ['running', 'canceled'], changeFilter: 'frontend' });
    assert.strictEqual(result.length, 2);
    assert.deepStrictEqual(result.map((r) => r.id), [3, 4]);
  });

  it('returns an empty array when no run matches', () => {
    const result = filterRuns(runs, { statusFilter: ['passed'], changeFilter: 'frontend' });
    assert.deepStrictEqual(result, []);
  });
});

describe('getUniqueRepos', () => {
  it('returns sorted unique repositories from all runs', () => {
    const result = getUniqueRepos(runs);
    assert.deepStrictEqual(result, ['auth', 'frontend']);
  });

  it('returns an empty array when no runs have changes', () => {
    const result = getUniqueRepos([{ id: 1, status: 'passed', changed: [] }]);
    assert.deepStrictEqual(result, []);
  });
});

describe('RUN_STATUSES', () => {
  it('contains the expected statuses', () => {
    assert.deepStrictEqual(RUN_STATUSES, [
      'passed', 'failed', 'running', 'queued', 'canceled', 'manual', 'skipped', 'pending',
    ]);
  });
});
