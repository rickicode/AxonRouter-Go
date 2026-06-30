// Simple path-based SPA router for Svelte 5 (History API)
import { writable, derived } from 'svelte/store';

export interface RouteParams {
  [key: string]: string;
}

export interface Route {
  path: string;
  params: RouteParams;
}

function getPath(): string {
  return window.location.pathname || '/';
}

function createRouter() {
  const current = writable<Route>({ path: getPath(), params: {} });

  function navigate(path: string) {
    if (path === getPath()) return;
    history.pushState(null, '', path);
    current.set({ path, params: {} });
  }

  function start() {
    const onPopState = () => {
      current.set({ path: getPath(), params: {} });
    };
    window.addEventListener('popstate', onPopState);
    // Set initial state
    current.set({ path: getPath(), params: {} });
    return () => window.removeEventListener('popstate', onPopState);
  }

  return { current, navigate, start };
}

export const router = createRouter();

// Derived store for current path
export const currentPath = derived(router.current, ($r) => $r.path);
