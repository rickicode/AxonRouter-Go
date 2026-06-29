// Theme store — dark mode only, no toggle
import { browser } from '$app/environment';

function applyDarkMode() {
  if (!browser) return;
  document.documentElement.classList.add('dark');
  document.documentElement.style.colorScheme = 'dark';
}

if (browser) {
  applyDarkMode();
}

// Export a no-op compatible store API so existing references don't break
import { readable } from 'svelte/store';

export const themeStore = {
  subscribe: readable({ isDark: true }).subscribe,
  toggle: () => {
    // Dark mode only — toggle disabled
    applyDarkMode();
  },
};
