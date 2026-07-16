export type ExpirationPreset = 'never' | '1d' | '7d' | '30d' | '90d' | 'custom';

export function buildExpiryTimestamp(
  preset: ExpirationPreset,
  customDate: string = '',
  nowMs: number = Date.now(),
): number | undefined {
  if (preset === 'never') return undefined;
  if (preset === 'custom') {
    if (!customDate) return undefined;
    return new Date(`${customDate}T23:59:59Z`).getTime() / 1000;
  }
  const days = parseInt(preset, 10);
  if (Number.isNaN(days)) return undefined;
  return nowMs / 1000 + days * 86400;
}

export function formatExpiry(timestamp: number | undefined, nowSec: number = Date.now() / 1000): string {
  if (!timestamp || timestamp <= 0) return 'Never';
  if (timestamp < nowSec) return 'Expired';
  const remaining = timestamp - nowSec;
  if (remaining < 60) return '<1m';
  if (remaining < 3600) return `${Math.round(remaining / 60)}m`;
  if (remaining < 86400) return `${Math.round(remaining / 3600)}h`;
  if (remaining < 604800) return `${Math.round(remaining / 86400)}d`;
  if (remaining < 2592000) return `${Math.round(remaining / 604800)}w`;
  return `${Math.round(remaining / 2592000)}mo`;
}
