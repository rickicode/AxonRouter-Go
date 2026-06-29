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

  onMount(() => {
    document.documentElement.classList.add('dark');
    document.documentElement.style.colorScheme = 'dark';
  });

  function getPageLabel(pathname: string): string {
    if (pathname === '/') return 'Dashboard';
    const segment = pathname.split('/').filter(Boolean)[0];
    const labels: Record<string, string> = {
      providers: 'Providers',
      combos: 'Combos',
      logs: 'Logs',
      settings: 'Settings',
    };
    return labels[segment] ?? segment.charAt(0).toUpperCase() + segment.slice(1);
  }
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
      <h1 class="text-body-md font-medium text-foreground">{getPageLabel($page.url.pathname)}</h1>
      <div class="ml-auto flex items-center gap-1.5 text-caption-mono text-muted-foreground">
        <span class="size-1.5 rounded-full bg-emerald-500 inline-block animate-pulse"></span>
        <span class="hidden sm:inline">Live</span>
      </div>
    </header>

    <main class="flex-1 overflow-auto">
      {@render children()}
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>
