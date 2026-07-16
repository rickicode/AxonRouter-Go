import { describe, it, expect } from 'vitest';
import { buildExpiryTimestamp, formatExpiry } from '$lib/api-key-utils';

describe('buildExpiryTimestamp', () => {
  it('returns undefined for "never"', () => {
    expect(buildExpiryTimestamp('never')).toBeUndefined();
  });

  it('returns now + N days for presets', () => {
    const before = Date.now() / 1000;
    const got = buildExpiryTimestamp('7d') ?? 0;
    const after = Date.now() / 1000;
    expect(got).toBeGreaterThanOrEqual(before + 7 * 86400);
    expect(got).toBeLessThanOrEqual(after + 7 * 86400);
  });

  it('returns end-of-day UTC for a custom date', () => {
    const got = buildExpiryTimestamp('custom', '2026-12-31');
    expect(got).toBe(new Date('2026-12-31T23:59:59Z').getTime() / 1000);
  });

  it('returns undefined for custom without a date', () => {
    expect(buildExpiryTimestamp('custom', '')).toBeUndefined();
  });
});

describe('formatExpiry', () => {
  it('formats zero as Never', () => {
    expect(formatExpiry(0)).toBe('Never');
  });

  it('formats undefined/null as Never', () => {
    expect(formatExpiry(undefined as unknown as number)).toBe('Never');
  });

  it('formats past timestamps as Expired', () => {
    expect(formatExpiry(1, 2)).toBe('Expired');
  });

  it('formats remaining durations in short units', () => {
    const now = 1000000;
    expect(formatExpiry(now + 30, now)).toBe('<1m');
    expect(formatExpiry(now + 120, now)).toBe('2m');
    expect(formatExpiry(now + 7200, now)).toBe('2h');
    expect(formatExpiry(now + 172800, now)).toBe('2d');
    expect(formatExpiry(now + 1209600, now)).toBe('2w');
  });
});
