import { describe, it } from 'node:test';
import assert from 'node:assert';
import {
  SKIP_CATEGORIES,
  formatSkipCategory,
  skipSeverity,
  getSkipSummary,
} from './skipReason.js';

describe('skipReason lib', () => {
  describe('formatSkipCategory', () => {
    it('formats known skip categories', () => {
      assert.strictEqual(formatSkipCategory(SKIP_CATEGORIES.TRIGGER_UNSATISFIED), 'trigger');
      assert.strictEqual(formatSkipCategory(SKIP_CATEGORIES.DEPENDENCY_FAILED), 'dep failed');
      assert.strictEqual(formatSkipCategory(SKIP_CATEGORIES.DEPENDENCY_CANCELED), 'dep canceled');
      assert.strictEqual(formatSkipCategory(SKIP_CATEGORIES.RUN_CANCELED), 'canceled');
    });

    it('falls back to custom category or skipped', () => {
      assert.strictEqual(formatSkipCategory('custom_reason'), 'custom_reason');
      assert.strictEqual(formatSkipCategory(null), 'skipped');
      assert.strictEqual(formatSkipCategory(undefined), 'skipped');
      assert.strictEqual(formatSkipCategory(''), 'skipped');
    });
  });

  describe('skipSeverity', () => {
    it('returns warning for dependency failures and cancellations', () => {
      assert.strictEqual(skipSeverity(SKIP_CATEGORIES.DEPENDENCY_FAILED), 'warning');
      assert.strictEqual(skipSeverity(SKIP_CATEGORIES.DEPENDENCY_CANCELED), 'warning');
    });

    it('returns info for trigger unsatisfied, run canceled, and default', () => {
      assert.strictEqual(skipSeverity(SKIP_CATEGORIES.TRIGGER_UNSATISFIED), 'info');
      assert.strictEqual(skipSeverity(SKIP_CATEGORIES.RUN_CANCELED), 'info');
      assert.strictEqual(skipSeverity('unknown'), 'info');
      assert.strictEqual(skipSeverity(undefined), 'info');
    });
  });

  describe('getSkipSummary', () => {
    it('returns summary if explicitly present on job.skip_reason', () => {
      const job = {
        skip_reason: {
          category: SKIP_CATEGORIES.TRIGGER_UNSATISFIED,
          summary: 'Custom trigger summary',
        },
      };
      assert.strictEqual(getSkipSummary(job), 'Custom trigger summary');
    });

    it('returns category fallback summaries when summary is omitted', () => {
      assert.strictEqual(
        getSkipSummary({ skip_reason: { category: SKIP_CATEGORIES.TRIGGER_UNSATISFIED } }),
        'Task skipped: trigger condition was not satisfied'
      );
      assert.strictEqual(
        getSkipSummary({ skip_reason: { category: SKIP_CATEGORIES.DEPENDENCY_FAILED } }),
        'Task skipped: upstream dependency failed'
      );
      assert.strictEqual(
        getSkipSummary({ skip_reason: { category: SKIP_CATEGORIES.DEPENDENCY_CANCELED } }),
        'Task skipped: upstream dependency was canceled'
      );
      assert.strictEqual(
        getSkipSummary({ skip_reason: { category: SKIP_CATEGORIES.RUN_CANCELED } }),
        'Task skipped: run was canceled'
      );
    });

    it('falls back to job.error or default message when category is unknown or absent', () => {
      assert.strictEqual(getSkipSummary({ error: 'custom error message' }), 'custom error message');
      assert.strictEqual(getSkipSummary({ skip_reason: { category: 'other' } }), 'Task skipped');
      assert.strictEqual(getSkipSummary({}), 'Task skipped');
      assert.strictEqual(getSkipSummary(null), 'Task skipped');
      assert.strictEqual(getSkipSummary(undefined), 'Task skipped');
    });
  });
});
