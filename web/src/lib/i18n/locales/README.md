# AxonRouter-Go locale dictionaries

Each file in this directory is a flat JSON object whose keys are `TranslationKey` dotted paths
(defined in `src/lib/i18n/messages.ts`) and whose values are the translated strings for that
locale, e.g.

```json
{
  "nav.dashboard": "仪表盘",
  "nav.providers": "提供商"
}
```

## Rules

1. **File name = locale code** (e.g. `zh-CN.json`, `id.json`, `ar.json`). See `config.ts` for the
   complete list.
2. **Flat JSON only** — keys are dotted paths, values are plain strings or template strings
   containing `{placeholder}` interpolation markers.
3. **Missing keys fall back to English** — you only need to include keys whose translation
   differs from the English default.
4. **A locale with no JSON file renders English** — every listed locale is usable; it simply
   shows English until someone drops in a JSON file.

## How to add a new locale

1. Create `<code>.json` in this directory.
2. Only include keys where the translation differs from English (or the entire English defaults
   are fine).
3. Rebuild the frontend (`cd web && npm run build`). The file is automatically picked up by
   Vite's `import.meta.glob` and served as a separate chunk.