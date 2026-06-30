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

    // Intercept all internal <a> clicks for SPA navigation
    const onClick = (e: MouseEvent) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      const a = (e.target as HTMLElement).closest('a');
      if (!a) return;
      const href = a.getAttribute('href');
      if (!href || href.startsWith('http') || href.startsWith('//') || href.startsWith('mailto:') || href.startsWith('tel:') || a.target === '_blank') return;
      e.preventDefault();
      navigate(href);
    };

    window.addEventListener('popstate', onPopState);
    document.addEventListener('click', onClick, true);
    // Set initial state
    current.set({ path: getPath(), params: {} });
    return () => {
      window.removeEventListener('popstate', onPopState);
      document.removeEventListener('click', onClick, true);
    };
  }

  return { current, navigate, start };
}

export const router = createRouter();

// Derived store for current path
export const currentPath = derived(router.current, ($r) => $r.path);
