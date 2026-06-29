<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { afterNavigate } from '$app/navigation';
  import type { Snippet } from 'svelte';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import { Toaster } from '$lib/components/ui/sonner';
  import SidebarNav from '$lib/components/sidebar/SidebarNav.svelte';
  import SidebarBrand from '$lib/components/sidebar/SidebarBrand.svelte';
  import SidebarHealth from '$lib/components/sidebar/SidebarHealth.svelte';

  let { children }: { children: Snippet } = $props();
</script>

<Toaster />
<Sidebar.Provider style="--sidebar-width: 16rem;">
  <Sidebar.Root collapsible="icon" class="border-sidebar-border/80">
    <Sidebar.Header>
      <SidebarBrand />
    </Sidebar.Header>
    <Sidebar.Content>
      <SidebarNav />
    </Sidebar.Content>
    <Sidebar.Footer>
      <SidebarHealth />
    </Sidebar.Footer>
    <Sidebar.Rail />
  </Sidebar.Root>
  
  <Sidebar.Inset>
    <main class="flex-1 overflow-auto">
      {@render children()}
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>
