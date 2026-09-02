// Reactive i18n for AxonRouter-Go dashboard.
//
// Provides a Svelte store `t` (callable as $t('key') in markup), a `locale` writable store,
// `setLocale()` for switching, and `initI18n()` for document-level wiring.
//
// Non-English dictionaries are loaded lazily via Vite import.meta.glob.  A locale with no JSON
// file produces no overrides, so t() falls back to the English default in messages.ts.
//
// localStorage persistence matches the axon_ prefix convention (cf. auth.ts).
// No server-side locale endpoint — this is a static SPA.

import { writable, derived, get } from 'svelte/store';
import { en, type TranslationKey } from './messages';
import {
  DEFAULT_LOCALE,
  LOCALES,
  LOCALE_NAMES,
  normalizeLocale,
  RTL_LOCALES,
  type LocaleCode,
} from './config';
import { toast } from 'svelte-sonner';

// ---------------------------------------------------------------------------
// Lazy dictionary loader — maps locale code → JSON module (or empty).
// Vite emits one chunk per file; the go:embed + NoRoute handler serves them.
// ---------------------------------------------------------------------------
interface DictionaryModule {
  default: Partial<Record<TranslationKey, string>>;
}
const dictGlob: Record<string, () => Promise<DictionaryModule>> = import.meta.glob('./locales/*.json');

function hasDictionary(code: LocaleCode): boolean {
  return code !== 'en' && `./locales/${code}.json` in dictGlob;
}

// Cache loaded dictionaries so we never fetch twice.
const dictCache = new Map<LocaleCode, Partial<Record<TranslationKey, string>>>();
dictCache.set('en', {}); // English is always empty overrides.

async function loadDictionary(code: LocaleCode): Promise<Partial<Record<TranslationKey, string>>> {
  if (code === 'en') return {};
  const cached = dictCache.get(code);
  if (cached) return cached;
  if (!hasDictionary(code)) {
    dictCache.set(code, {});
    return {};
  }
  try {
    const mod = await dictGlob[`./locales/${code}.json`]();
    const dict: Partial<Record<TranslationKey, string>> = mod.default ?? {};
    dictCache.set(code, dict);
    return dict;
  } catch {
    // File missing or corrupt — fall back to English.
    dictCache.set(code, {});
    return {};
  }
}

// ---------------------------------------------------------------------------
// Locale persistence
// ---------------------------------------------------------------------------
const STORAGE_KEY = 'axon_locale';

function readStoredLocale(): LocaleCode | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (!v) return null;
    const normalized = normalizeLocale(v);
    return normalized === DEFAULT_LOCALE ? null : normalized;
  } catch {
    return null;
  }
}

function writeStoredLocale(code: LocaleCode): void {
  try {
    localStorage.setItem(STORAGE_KEY, code);
  } catch {
    // Storage may be unavailable (private browsing, noop polyfill).
  }
}

/** Resolve initial locale: stored → browser → default. */
function resolveInitialLocale(): LocaleCode {
  const stored = readStoredLocale();
  if (stored) return stored;
  if (typeof navigator !== 'undefined') {
    // navigator.language may have a region (en-US) — normalizeLocale handles that.
    return normalizeLocale(navigator.language);
  }
  return DEFAULT_LOCALE;
}

// ---------------------------------------------------------------------------
// Interpolation helper
// ---------------------------------------------------------------------------
function interpolate(tpl: string, vars?: Record<string, string | number>): string {
  if (!vars) return tpl;
  return tpl.replace(/\{(\w+)\}/g, (_, key: string) => {
    const val = vars[key];
    return val !== undefined ? String(val) : `{${key}}`;
  });
}

// ---------------------------------------------------------------------------
// Deep property lookup
// ---------------------------------------------------------------------------
function lookup(obj: Record<string, unknown>, path: string): string | undefined {
  const parts = path.split('.');
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur === null || cur === undefined || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return typeof cur === 'string' ? cur : undefined;
}

// ---------------------------------------------------------------------------
// Public exports
// ---------------------------------------------------------------------------

/** Current locale code. Set via setLocale(). */
export const locale = writable<LocaleCode>(resolveInitialLocale());

/** Live overrides from the active non-English dictionary (empty for English). */
export const overrides = writable<Partial<Record<TranslationKey, string>>>({});

/**
 * Reactive translation function. Use as `$t('nav.dashboard')` in Svelte markup or
 * `get(t)('common.cancel')` in script.
 */
export const t = derived(
  [locale, overrides],
  ([$locale, $overrides]) =>
    (key: TranslationKey, vars?: Record<string, string | number>): string => {
      const override = $overrides[key];
      if (override !== undefined) return interpolate(override, vars);
      const fallback = lookup(en as unknown as Record<string, unknown>, key);
      if (fallback !== undefined) return interpolate(fallback, vars);
      return key; // never throws, never renders undefined
    },
);

/** Derived direction — flips layout when a RTL locale is active. */
export const direction = derived(locale, ($l) => (RTL_LOCALES.has($l) ? 'rtl' : 'ltr'));

/** Locale metadata including whether a dictionary file exists. */
export interface LocaleMeta {
  code: LocaleCode;
  name: string;
  translated: boolean;
}

/**
 * All 35 locales with translation-availability flags. A locale counts as
 * translated only if a JSON dictionary exists in src/lib/i18n/locales/.
 * English is the source of truth and is always "translated".
 */
export const availableLocales: LocaleMeta[] = (() => {
  const available = new Set<string>();
  for (const k of Object.keys(dictGlob)) {
    const m = k.match(/\.\/locales\/(.+)\.json$/);
    if (m) available.add(m[1]);
  }
  return LOCALES.map((code) => ({
    code,
    name: LOCALE_NAMES[code] ?? code,
    translated: code === 'en' || available.has(code),
  }));
})();

/**
 * Switch to a different locale. Loads the dictionary if not cached, persists
 * the choice, updates document lang/dir, and fires a success toast.
 */
export async function setLocale(code: LocaleCode): Promise<void> {
  const dict = await loadDictionary(code);
  locale.set(code);
  overrides.set(dict);
  writeStoredLocale(code);
  try {
    document.documentElement.lang = code;
    document.documentElement.dir = RTL_LOCALES.has(code) ? 'rtl' : 'ltr';
  } catch {
    // SSR / test — document may not exist.
  }
  toast.success(`Language: ${LOCALE_NAMES[code] ?? code}`);
}

/**
 * One-time initialization: applies lang/dir to the document element and keeps
 * them in sync with the locale store. Call from App.svelte's onMount.
 */
export function initI18n(): (() => void) {
  const apply = ($locale: LocaleCode) => {
    try {
      document.documentElement.lang = $locale;
      document.documentElement.dir = RTL_LOCALES.has($locale) ? 'rtl' : 'ltr';
    } catch {
      // SSR / test — document may not exist.
    }
  };
  apply(get(locale));
  return locale.subscribe(apply);
}

/**
 * Get the current t() function for script-only contexts (stores.ts, api.ts).
 * Equivalent to get(t) but with a cleaner signature.
 */
export function getT(): (key: TranslationKey, vars?: Record<string, string | number>) => string {
  return get(t);
}