// Theme store — DESIGN.md light mode
// No dark mode toggle; light canvas matches Vercel design system

import { readable } from 'svelte/store';

export const themeStore = {
  subscribe: readable({ isDark: false }).subscribe,
  toggle: () => {},
};
