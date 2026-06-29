<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import type { Snippet } from 'svelte';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import { Toaster } from '$lib/components/ui/sonner';
  import SidebarNav from '$lib/components/sidebar/SidebarNav.svelte';
  import SidebarBrand from '$lib/components/sidebar/SidebarBrand.svelte';
  import SidebarHealth from '$lib/components/sidebar/SidebarHealth.svelte';
  import { page } from '$app/stores';

  let { children }: { children: Snippet } = $props();

  // Always force dark mode
  onMount(() => {
    document.documentElement.classList.add('dark');
    document.documentElement.style.colorScheme = 'dark';
  });

  // Derive page label from current route
  function getPageLabel(pathname: string): string {
    if (pathname === '/') return 'Dashboard';
    const segment = pathname.split('/').filter(Boolean)[0];
    const labels: Record<string, string> = {
      endpoint: 'Endpoint',
      providers: 'Providers',
      combos: 'Combos',
      logs: 'Logs',
      quota: 'Quota Tracker',
      usage: 'Usage Logs',
      analytics: 'Analytics',
      'proxy-pools': 'Proxy Pools',
      settings: 'Settings',
    };
    return labels[segment] ?? segment.charAt(0).toUpperCase() + segment.slice(1);
  }
</script>

<Toaster />
<Sidebar.Provider style="--sidebar-width: 16rem;">
  <!--
    Single Sidebar.Root keeps the peer CSS relationship intact for Sidebar.Inset.
    collapsible="offcanvas" means it is fixed/visible on desktop and offcanvas sheet on mobile.
    Since we hide Sidebar.Trigger on desktop, the user cannot collapse it.
  -->
  <Sidebar.Root
    collapsible="offcanvas"
    class="border-r border-sidebar-border"
  >
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
    <!-- Top header bar -->
    <header class="flex h-14 shrink-0 items-center justify-between gap-2 border-b border-border bg-background/50 backdrop-blur-md sticky top-0 z-50 px-6">
      <div class="flex items-center gap-2">
        <!-- Mobile trigger: only visible on screens < md -->
        <Sidebar.Trigger class="md:hidden text-muted-foreground hover:text-foreground transition-colors cursor-pointer" />
        <span class="text-border select-none hidden md:inline">/</span>
        <div class="flex items-center gap-1.5 text-body-sm font-medium">
          <span class="text-muted-foreground">personal</span>
          <span class="text-muted-foreground/40 select-none">/</span>
          <span class="text-foreground font-medium">axonrouter</span>
          {#if $page.url.pathname !== '/'}
            <span class="text-muted-foreground/40 select-none">/</span>
            <span class="text-foreground/80">{getPageLabel($page.url.pathname)}</span>
          {/if}
        </div>
      </div>

      <!-- Right side: status indicator only -->
      <div class="flex items-center gap-3">
        <div class="flex items-center gap-1.5 text-caption-mono text-muted-foreground">
          <span class="size-1.5 rounded-full bg-emerald-500 inline-block animate-pulse"></span>
          <span class="hidden sm:inline">Live</span>
        </div>
      </div>
    </header>

    <main class="flex-1 overflow-auto">
      {@render children()}
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>
