import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";
import type { HTMLAttributes, HTMLInputAttributes, HTMLTextareaAttributes, HTMLSelectAttributes, HTMLButtonAttributes } from "svelte/elements";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(...inputs));
}

// ── shadcn-svelte helper types ──────────────────────────────────────────
// These types are expected by generated shadcn components for ref/class props.

type RefProp<T> = { ref?: T | null };

export type WithElementRef<T, RefType extends HTMLElement = HTMLElement> = T & RefProp<RefType>;

export type WithoutChildren<T> = Omit<T, 'children'>;
export type WithoutChild<T> = Omit<T, 'child'>;
export type WithoutChildrenOrChild<T> = Omit<T, 'children' | 'child'>;

// Unwrap Go sql.NullInt64 → number | null
// ponytail: handles both raw number and {Int64, Valid} shape from Go JSON
export function unwrapInt(v: unknown): number | null {
  if (v == null) return null;
  if (typeof v === 'number') return v;
  if (typeof v === 'object' && v !== null && 'Valid' in v) {
    const nv = v as unknown as { Valid: boolean; Int64: number };
    return nv.Valid ? nv.Int64 : null;
  }
  return null;
}

// Unwrap Go sql.NullString → string | null
export function unwrapStr(v: unknown): string | null {
  if (v == null) return null;
  if (typeof v === 'string') return v;
  if (typeof v === 'object' && v !== null && 'Valid' in v) {
    const nv = v as unknown as { Valid: boolean; String: string };
    return nv.Valid ? nv.String : null;
  }
  return null;
}

export type TokenExpiryInfo = { status: 'expired' | 'expiring' | 'valid'; text: string };

// Centralized copy helper: works on both HTTPS and HTTP deployments.
// HTTPS/localhost uses the modern Clipboard API; everything else falls back
// to a temporary textarea + execCommand so the dashboard still works on
// plain HTTP LAN installs.
export async function copyToClipboard(text: string): Promise<void> {
	if (!text) return;
	if (navigator.clipboard && window.isSecureContext) {
		await navigator.clipboard.writeText(text);
		return;
	}
	const ta = document.createElement('textarea');
	ta.value = text;
	ta.setAttribute('readonly', '');
	ta.style.position = 'fixed';
	ta.style.left = '-9999px';
	ta.style.opacity = '0';
	document.body.appendChild(ta);
	ta.select();
	try {
		const ok = document.execCommand('copy');
		document.body.removeChild(ta);
		if (!ok) throw new Error('execCommand failed');
	} catch (err) {
		document.body.removeChild(ta);
		throw err;
	}
}

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
