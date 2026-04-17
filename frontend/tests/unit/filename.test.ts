import { describe, it, expect } from 'vitest';
import { sanitizeCustomer, buildFilename } from '@/lib/filename';

/**
 * The sanitizer MUST produce the same output as the backend regex
 * `[\\/:*?"<>|\s]+` → `_`, trimmed, with `未知客户` fallback. These tests
 * are the canonical contract check between frontend and backend.
 */
describe('sanitizeCustomer', () => {
  it('replaces a single illegal char with an underscore', () => {
    expect(sanitizeCustomer('A/B')).toBe('A_B');
  });

  it('collapses consecutive illegal chars into one underscore', () => {
    expect(sanitizeCustomer('A  B')).toBe('A_B');
    expect(sanitizeCustomer('A \t\nB')).toBe('A_B');
    expect(sanitizeCustomer('A/ \\ :B')).toBe('A_B');
  });

  it('trims trailing / leading underscores', () => {
    expect(sanitizeCustomer(' 哈尔滨 金诺 ')).toBe('哈尔滨_金诺');
    expect(sanitizeCustomer('///abc///')).toBe('abc');
  });

  it('handles every forbidden Windows/macOS char class', () => {
    expect(sanitizeCustomer('a\\b/c:d*e?f"g<h>i|j k')).toBe('a_b_c_d_e_f_g_h_i_j_k');
  });

  it('falls back to 未知客户 for null/empty/whitespace-only input', () => {
    expect(sanitizeCustomer('')).toBe('未知客户');
    expect(sanitizeCustomer(null)).toBe('未知客户');
    expect(sanitizeCustomer(undefined)).toBe('未知客户');
    expect(sanitizeCustomer('   ')).toBe('未知客户');
    expect(sanitizeCustomer('///')).toBe('未知客户');
  });

  it('preserves normal Chinese names untouched', () => {
    expect(sanitizeCustomer('哈尔滨金诺食品有限公司')).toBe('哈尔滨金诺食品有限公司');
  });
});

describe('buildFilename', () => {
  it('zero-pads seq to 2 digits and joins with dashes', () => {
    expect(buildFilename('RX2101-22926', '哈尔滨 金诺', '合同', 3)).toBe(
      'RX2101-22926-哈尔滨_金诺-合同-03.jpg',
    );
  });

  it('falls back to 未知客户 for empty customer', () => {
    expect(buildFilename('RX2101-22926', '', '发票', 1)).toBe(
      'RX2101-22926-未知客户-发票-01.jpg',
    );
  });

  it('pads seq ≥ 10 without extra zeros', () => {
    expect(buildFilename('A1', 'Alpha', '发货单', 12)).toBe('A1-Alpha-发货单-12.jpg');
  });
});
