import { describe, it } from 'node:test';
import assert from 'node:assert';
import { clampOffset, parseOffsetFromUrl, getPaginationInfo } from '../lib/paginationUtils.js';

describe('clampOffset', () => {
  it('returns value when within bounds', () => {
    assert.strictEqual(clampOffset(5, 20), 5);
  });

  it('returns 0 when value is negative', () => {
    assert.strictEqual(clampOffset(-10, 20), 0);
  });

  it('returns total when value exceeds total', () => {
    assert.strictEqual(clampOffset(30, 20), 20);
  });

  it('returns 0 when both value and total are 0', () => {
    assert.strictEqual(clampOffset(0, 0), 0);
  });
});

describe('parseOffsetFromUrl', () => {
  it('returns offset from a valid URL', () => {
    const result = parseOffsetFromUrl('http://localhost/api/projects?limit=25&offset=50');
    assert.strictEqual(result, 50);
  });

  it('returns null when no offset param', () => {
    const result = parseOffsetFromUrl('http://localhost/api/projects?limit=25');
    assert.strictEqual(result, null);
  });

  it('returns null when urlString is null', () => {
    assert.strictEqual(parseOffsetFromUrl(null), null);
  });

  it('returns null when urlString is empty', () => {
    assert.strictEqual(parseOffsetFromUrl(''), null);
  });

  it('returns null for malformed URL', () => {
    const result = parseOffsetFromUrl('http://[::1');
    assert.strictEqual(result, null);
  });
});

describe('getPaginationInfo', () => {
  it('computes page 1 when offset is 0', () => {
    const info = getPaginationInfo({ total: 100, limit: 25, offset: 0 });
    assert.deepStrictEqual(info, { currentPage: 1, totalPages: 4 });
  });

  it('computes page 2 when offset is 25', () => {
    const info = getPaginationInfo({ total: 100, limit: 25, offset: 25 });
    assert.deepStrictEqual(info, { currentPage: 2, totalPages: 4 });
  });

  it('computes last page when offset equals total minus limit', () => {
    const info = getPaginationInfo({ total: 100, limit: 25, offset: 75 });
    assert.deepStrictEqual(info, { currentPage: 4, totalPages: 4 });
  });

  it('computes single page when total is less than limit', () => {
    const info = getPaginationInfo({ total: 5, limit: 25, offset: 0 });
    assert.deepStrictEqual(info, { currentPage: 1, totalPages: 1 });
  });

  it('computes correct pages for zero items', () => {
    const info = getPaginationInfo({ total: 0, limit: 25, offset: 0 });
    assert.deepStrictEqual(info, { currentPage: 1, totalPages: 1 });
  });

  it('computes fractional total page correctly', () => {
    const info = getPaginationInfo({ total: 30, limit: 25, offset: 0 });
    assert.deepStrictEqual(info, { currentPage: 1, totalPages: 2 });
  });
});