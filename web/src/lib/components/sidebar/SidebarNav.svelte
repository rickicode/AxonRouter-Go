<script lang="ts">
  import { currentPath, router } from '$lib/router';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import {
    Home,
    Server,
    Layers,
    Terminal,
    Settings,
    Gauge,
    Globe,
    Key,
    MessageSquare,
  } from '@lucide/svelte';

  let { onclose }: { onclose?: () => void } = $props();

  const platformItems = [
    { href: '/', label: 'Dashboard', icon: Home },
    { href: '/providers', label: 'Providers', icon: Server },
    { href: '/combos', label: 'Combos', icon: Layers },
    { href: '/quota', label: 'Quota', icon: Gauge },
    { href: '/context', label: 'Context & Cache', icon: MessageSquare },
    { href: '/logs', label: 'Logs', icon: Terminal },
  ];

  const settingsItem = { href: '/settings', label: 'Settings', icon: Settings };
  const apiKeysItem = { href: '/api-keys', label: 'API Keys', icon: Key };

  const systemItems = [
    { href: '/proxy-pools', label: 'Proxy Pools', icon: Globe },
    apiKeysItem,
    settingsItem,
  ];

  function isActive(pathname: string, href: string): boolean {
    if (href === '/') return pathname === '/';
    return pathname === href || pathname.startsWith(href + '/');
  }

  function handleClick(e: MouseEvent, href: string) {
    e.preventDefault();
    router.navigate(href);
    onclose?.();
  }

</script>

<div class="flex flex-col gap-1">
  <!-- Platform Section -->
  <div class="mb-1">
    <p class="px-3 py-1.5 text-caption-mono uppercase tracking-wider text-sidebar-foreground/30 select-none">
      Platform
    </p>
    <nav class="space-y-0.5">
      {#each platformItems as item}
        {@const active = isActive($currentPath, item.href)}
        <a
          href={item.href}
          onclick={(e) => handleClick(e, item.href)}
          class="group relative flex items-center gap-3 rounded-md px-3 py-2 text-body-sm-strong transition-all duration-150
            {active
              ? 'bg-sidebar-accent text-sidebar-foreground'
              : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150
            {active ? 'bg-sidebar-primary opacity-100' : 'opacity-0'}"></span>
          <item.icon
            class="size-4 shrink-0 transition-colors duration-150
              {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}"
          />
          <span class="truncate">{item.label}</span>
          {#if active}
            <span class="ml-auto size-1.5 rounded-full bg-sidebar-primary shrink-0"></span>
          {/if}
        </a>
      {/each}
    </nav>
  </div>

  <div class="my-1 border-t border-sidebar-border/50"></div>

  <!-- System Section -->
  <div>
    <p class="px-3 py-1.5 text-caption-mono uppercase tracking-wider text-sidebar-foreground/30 select-none">
      System
    </p>
    <nav class="space-y-0.5">
      {#each systemItems as item}
        {@const active = isActive($currentPath, item.href)}
        <a
          href={item.href}
          onclick={(e) => handleClick(e, item.href)}
          class="group relative flex items-center gap-3 rounded-md px-3 py-2 text-body-sm-strong transition-all duration-150
            {active
              ? 'bg-sidebar-accent text-sidebar-foreground'
              : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150
            {active ? 'bg-sidebar-primary opacity-100' : 'opacity-0'}"></span>
          <item.icon
            class="size-4 shrink-0 transition-colors duration-150
              {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}"
          />
          <span class="truncate">{item.label}</span>
          {#if active}
            <span class="ml-auto size-1.5 rounded-full bg-sidebar-primary shrink-0"></span>
          {/if}
        </a>
      {/each}
    </nav>
  </div>
</div>
