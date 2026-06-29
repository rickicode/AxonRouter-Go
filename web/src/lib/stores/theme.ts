// Theme store with localStorage persistence

import { writable } from 'svelte/store';
import { browser } from '$app/environment';

interface ThemeState {
  isDark: boolean;
}

const STORAGE_KEY = 'axonrouter_theme';

function createThemeStore() {
  const defaultState: ThemeState = { isDark: true };

  let initialState = defaultState;
  if (browser) {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) {
        const parsed = JSON.parse(stored);
        initialState = { isDark: parsed.isDark ?? true };
      }
    } catch {
      // Ignore parse errors, use default
    }
  }

  const { subscribe, set, update } = writable<ThemeState>(initialState);

  function applyTheme(isDark: boolean) {
    if (!browser) return;
    document.documentElement.classList.toggle('dark', isDark);
    document.documentElement.style.colorScheme = isDark ? 'dark' : 'light';
  }

  if (browser) {
    applyTheme(initialState.isDark);
  }

  return {
    subscribe,

    toggle() {
      update((state) => {
        const newState = { isDark: !state.isDark };
        if (browser) {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(newState));
          applyTheme(newState.isDark);
        }
        return newState;
      });
    },
  };
}

export const themeStore = createThemeStore();
