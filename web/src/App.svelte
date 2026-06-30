<script lang="ts">
  import './app.css';
  import { onMount } from 'svelte';
  import { router, currentPath } from '$lib/router';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import { Toaster } from '$lib/components/ui/sonner';
  import SidebarNav from '$lib/components/sidebar/SidebarNav.svelte';
  import SidebarBrand from '$lib/components/sidebar/SidebarBrand.svelte';
  import SidebarHealth from '$lib/components/sidebar/SidebarHealth.svelte';

  // Page components
  import Dashboard from './pages/Dashboard.svelte';
  import Providers from './pages/Providers.svelte';
  import ProviderDetail from './pages/ProviderDetail.svelte';
  import ConnectionDetail from './pages/ConnectionDetail.svelte';
  import ProviderAdd from './pages/ProviderAdd.svelte';
  import Combos from './pages/Combos.svelte';
  import ComboDetail from './pages/ComboDetail.svelte';
  import Logs from './pages/Logs.svelte';
  import Settings from './pages/Settings.svelte';

  let cleanup: (() => void) | undefined;

  onMount(() => {
    cleanup = router.start();
    return () => cleanup?.();
  });

  function getPageLabel(path: string): string {
    if (path === '/') return 'Dashboard';
    const segment = path.split('/').filter(Boolean)[0];
    const labels: Record<string, string> = {
      providers: 'Providers',
      combos: 'Combos',
      logs: 'Logs',
      settings: 'Settings',
    };
    return labels[segment] ?? segment.charAt(0).toUpperCase() + segment.slice(1);
  }

  function matchRoute(path: string): { component: typeof Dashboard; params: Record<string, string> } {
    const segments = path.split('/').filter(Boolean);

    // / → Dashboard
    if (segments.length === 0) return { component: Dashboard, params: {} };

    // /providers → Providers
    if (segments[0] === 'providers' && segments.length === 1) return { component: Providers, params: {} };

    // /providers/add → ProviderAdd
    if (segments[0] === 'providers' && segments[1] === 'add') return { component: ProviderAdd, params: {} };

    // /providers/:id/:connId → ConnectionDetail
    if (segments[0] === 'providers' && segments.length === 3) return { component: ConnectionDetail, params: { id: segments[1], connId: segments[2] } };

    // /providers/:id → ProviderDetail
    if (segments[0] === 'providers' && segments.length === 2) return { component: ProviderDetail, params: { id: segments[1] } };

    // /combos → Combos
    if (segments[0] === 'combos' && segments.length === 1) return { component: Combos, params: {} };

    // /combos/:id → ComboDetail
    if (segments[0] === 'combos' && segments.length === 2) return { component: ComboDetail, params: { id: segments[1] } };

    // /logs → Logs
    if (segments[0] === 'logs') return { component: Logs, params: {} };

    // /settings → Settings
    if (segments[0] === 'settings') return { component: Settings, params: {} };

    // Fallback
    return { component: Dashboard, params: {} };
  }

  let route = $derived(matchRoute($currentPath));
  let pageLabel = $derived(getPageLabel($currentPath));
</script>

<Toaster />
<Sidebar.Provider style="--sidebar-width: 16rem;">
  <Sidebar.Root collapsible="offcanvas" class="border-r border-sidebar-border">
    <Sidebar.Header class="border-b border-sidebar-border/50 px-2 py-3">
      <SidebarBrand />
    </Sidebar.Header>
    <Sidebar.Content class="px-2 py-3">
      <SidebarNav />
    </Sidebar.Content>
    <Sidebar.Footer class="border-t border-sidebar-border/50">
      <SidebarHealth />
    </Sidebar.Footer>
  </Sidebar.Root>

  <Sidebar.Inset>
    <header class="flex h-14 shrink-0 items-center gap-2 border-b border-border bg-background/50 backdrop-blur-md sticky top-0 z-50 px-6">
      <Sidebar.Trigger class="md:hidden text-muted-foreground hover:text-foreground transition-colors cursor-pointer" />
      <h1 class="text-body-md font-medium text-foreground">{pageLabel}</h1>
      <div class="ml-auto flex items-center gap-1.5 text-caption-mono text-muted-foreground">
        <span class="size-1.5 rounded-full bg-emerald-500 inline-block animate-pulse"></span>
        <span class="hidden sm:inline">Live</span>
      </div>
    </header>

    <main class="flex-1 overflow-auto">
      <route.component {...route.params} />
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>
