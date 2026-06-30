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
