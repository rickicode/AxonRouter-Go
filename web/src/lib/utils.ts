import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// Unwrap Go sql.NullInt64 → number | null
// ponytail: handles both raw number and {Int64, Valid} shape from Go JSON
export function unwrapInt(v: unknown): number | null {
  if (v == null) return null;
  if (typeof v === 'number') return v;
  if (typeof v === 'object' && 'Valid' in v) {
    return (v as { Valid: boolean; Int64: number }).Valid ? (v as { Int64: number }).Int64 : null;
  }
  return null;
}

// Unwrap Go sql.NullString → string | null
export function unwrapStr(v: unknown): string | null {
  if (v == null) return null;
  if (typeof v === 'string') return v;
  if (typeof v === 'object' && 'Valid' in v) {
    return (v as { Valid: boolean; String: string }).Valid ? (v as { String: string }).String : null;
  }
  return null;
}

export type TokenExpiryInfo = { status: 'expired' | 'expiring' | 'valid'; text: string };

// Centralized token expiry display. Uses ceil so sub-minute future tokens
// show "~1m" instead of "~0m". Returns null when not applicable.
export function getTokenExpiry(oauthExpiresAt: unknown): TokenExpiryInfo | null {
  const raw = unwrapInt(oauthExpiresAt);
  if (raw == null || raw <= 0) return null;
  const msLeft = raw * 1000 - Date.now();
  if (msLeft <= 0) return { status: 'expired', text: 'Expired' };
  const minsLeft = Math.ceil(msLeft / 60000);
  if (minsLeft < 30) return { status: 'expiring', text: `~${minsLeft}m` };
  const h = Math.floor(minsLeft / 60);
  const m = minsLeft % 60;
  return { status: 'valid', text: `${h}h ${m}m` };
}
