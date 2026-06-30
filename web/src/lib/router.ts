// Simple hash-based SPA router for Svelte 5
import { writable, derived } from 'svelte/store';

export interface RouteParams {
  [key: string]: string;
}

export interface Route {
  path: string;
  params: RouteParams;
}

function parseHash(hash: string): Route {
  const path = hash.replace(/^#\/?/, '/') || '/';
  const segments = path.split('/').filter(Boolean);
  return { path, params: {} };
}

function createRouter() {
  const current = writable<Route>(parseHash(window.location.hash));

  function navigate(hash: string) {
    window.location.hash = hash;
  }

  function start() {
    const onHashChange = () => {
      current.set(parseHash(window.location.hash));
    };
    window.addEventListener('hashchange', onHashChange);
    // Set initial hash if empty
    if (!window.location.hash) {
      window.location.hash = '#/';
    }
    return () => window.removeEventListener('hashchange', onHashChange);
  }

  return { current, navigate, start };
}

export const router = createRouter();

// Derived store for current path
export const currentPath = derived(router.current, ($r) => $r.path);
