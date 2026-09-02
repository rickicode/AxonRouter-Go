// i18n locale configuration — ported from 9router-mibp-version/src/i18n/config.js (35 locales).
// English (en) is the default and is implicit: there is no en.json dictionary, English is the
// source of truth in messages.ts.

export const LOCALES = [
  'en',
  'vi',
  'zh-CN',
  'zh-TW',
  'ja',
  'pt-BR',
  'pt-PT',
  'ko',
  'es',
  'de',
  'fr',
  'he',
  'ar',
  'ru',
  'pl',
  'cs',
  'nl',
  'tr',
  'uk',
  'tl',
  'id',
  'km',
  'th',
  'hi',
  'bn',
  'ur',
  'ro',
  'sv',
  'it',
  'el',
  'hu',
  'fi',
  'da',
  'no',
  'fa',
] as const;

export type LocaleCode = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: LocaleCode = 'en';

// Locales rendered right-to-left. Only relevant once a dictionary exists; wired now so the
// document dir attribute flips the instant an ar/he/fa translation lands.
export const RTL_LOCALES = new Set<LocaleCode>(['ar', 'he', 'fa']);

export const LOCALE_NAMES: Record<LocaleCode, string> = {
  en: 'English',
  vi: 'Tiếng Việt',
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  ja: '日本語',
  'pt-BR': 'Português (Brasil)',
  'pt-PT': 'Português (Portugal)',
  ko: '한국어',
  es: 'Español',
  de: 'Deutsch',
  fr: 'Français',
  he: 'עברית',
  ar: 'العربية',
  ru: 'Русский',
  pl: 'Polski',
  cs: 'Čeština',
  nl: 'Nederlands',
  tr: 'Türkçe',
  uk: 'Українська',
  tl: 'Tagalog',
  id: 'Indonesia',
  th: 'ไทย',
  km: 'ខ្មែរ',
  hi: 'हिन्दी',
  bn: 'বাংলা',
  ur: 'اردو',
  ro: 'Română',
  sv: 'Svenska',
  it: 'Italiano',
  el: 'Ελληνικά',
  hu: 'Magyar',
  fi: 'Suomi',
  da: 'Dansk',
  no: 'Norsk',
  fa: 'فارسی',
};

/** Exact membership check — no aliasing. */
export function isSupportedLocale(locale: string | null | undefined): locale is LocaleCode {
  return typeof locale === 'string' && (LOCALES as readonly string[]).includes(locale);
}

/**
 * Normalize a raw locale candidate (browser `navigator.language`, a stored value, a URL param)
 * into a supported LocaleCode. Mirrors 9router's behavior: bare `zh` maps to `zh-CN`; anything
 * unsupported falls back to the default (`en`). Browser tags that carry a region (e.g. `pt-BR`,
 * `zh-TW`) are kept when they match; a prefix like `en-US` matches its base `en`.
 */
export function normalizeLocale(raw: string | null | undefined): LocaleCode {
  if (isSupportedLocale(raw)) return raw;
  if (typeof raw !== 'string') return DEFAULT_LOCALE;
  const tag = raw.trim();
  if (tag === 'zh') return 'zh-CN';
  // Match by base language for tags with a region suffix (en-US → en, pt-BR → pt-BR kept above).
  const base = tag.toLowerCase().split(/[-_]/)[0];
  if (base === 'zh') return 'zh-CN';
  const match = LOCALES.find((code) => code.toLowerCase() === tag.toLowerCase());
  if (match) return match;
  const baseMatch = LOCALES.find((code) => code.split('-')[0].toLowerCase() === base);
  return baseMatch ?? DEFAULT_LOCALE;
}
