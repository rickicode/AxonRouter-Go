// Theme store — dark mode only, no toggle
function applyDarkMode() {
  document.documentElement.classList.add('dark');
  document.documentElement.style.colorScheme = 'dark';
}

applyDarkMode();

// Export a no-op compatible store API so existing references don't break
import { readable } from 'svelte/store';

export const themeStore = {
  subscribe: readable({ isDark: true }).subscribe,
  toggle: () => {
    applyDarkMode();
  },
};
