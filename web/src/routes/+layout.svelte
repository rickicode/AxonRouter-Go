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
  import { themeStore } from '$lib/stores/theme';

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
    <header class="flex h-14 shrink-0 items-center justify-between gap-2 border-b border-border bg-background/70 backdrop-blur-md sticky top-0 z-50 px-6">
      <div class="flex items-center gap-2">
        <Sidebar.Trigger class="-ml-1 text-muted-foreground hover:text-foreground transition-colors cursor-pointer" />
        <span class="text-border select-none">/</span>
        <div class="flex items-center gap-1.5 text-body-sm font-medium">
          <span class="text-muted-foreground hover:text-foreground cursor-pointer transition-colors">personal</span>
          <span class="text-muted-foreground/40 select-none">/</span>
          <span class="text-foreground select-none">axonrouter</span>
        </div>
      </div>
      
      <div class="flex items-center gap-4">
        <button
          onclick={() => themeStore.toggle()}
          class="size-8 rounded-md border border-border bg-card flex items-center justify-center text-muted-foreground hover:text-foreground transition-all hover:bg-accent cursor-pointer"
          aria-label="Toggle theme"
        >
          {#if $themeStore.isDark}
            <!-- Sun icon -->
            <svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364-6.364l-.707.707M6.343 17.657l-.707.707m12.728 0l-.707-.707M6.343 6.364l-.707-.707M12 8a4 4 0 100 8 4 4 0 000-8z" />
            </svg>
          {:else}
            <!-- Moon icon -->
            <svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
            </svg>
          {/if}
        </button>
      </div>
    </header>
    
    <main class="flex-1 overflow-auto">
      {@render children()}
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>

