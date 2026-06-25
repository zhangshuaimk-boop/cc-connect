import { describe, expect, it } from 'vitest';
import { cn, formatTime, formatUptime, truncate } from './utils';

describe('utils', () => {
  it('formats uptime using the largest useful units', () => {
    expect(formatUptime(59)).toBe('0m');
    expect(formatUptime(60)).toBe('1m');
    expect(formatUptime(3660)).toBe('1h 1m');
    expect(formatUptime(90060)).toBe('1d 1h 1m');
  });

  it('formats empty time as a placeholder and non-empty time through Date', () => {
    expect(formatTime('')).toBe('-');
    expect(formatTime('2026-06-25T00:00:00Z')).not.toBe('-');
  });

  it('truncates only strings longer than the maximum length', () => {
    expect(truncate('hello', 10)).toBe('hello');
    expect(truncate('hello world', 5)).toBe('hello...');
    expect(truncate('hello', 5)).toBe('hello');
  });

  it('merges class values with clsx semantics', () => {
    expect(cn('base', false && 'hidden', { active: true, disabled: false })).toBe('base active');
  });
});
