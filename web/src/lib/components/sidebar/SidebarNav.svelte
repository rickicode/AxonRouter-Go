<script lang="ts">
  import { page } from '$app/stores';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import {
    Home,
    Server,
    Layers,
    Terminal,
    Settings,
    Globe,
    Gauge,
    BarChart3,
    FolderCog,
  } from '@lucide/svelte';

  let { onclose }: { onclose?: () => void } = $props();

  const platformItems = [
    { href: '/', label: 'Dashboard', icon: Home },
    { href: '/endpoint', label: 'Endpoint', icon: Globe },
    { href: '/providers', label: 'Providers', icon: Server },
    { href: '/combos', label: 'Combos', icon: Layers },
    { href: '/logs', label: 'Logs', icon: Terminal },
  ];

  const operationsItems = [
    { href: '/quota', label: 'Quota Tracker', icon: Gauge },
    { href: '/usage', label: 'Usage Logs', icon: BarChart3 },
    { href: '/analytics', label: 'Analytics', icon: BarChart3 },
    { href: '/proxy-pools', label: 'Proxy Pools', icon: FolderCog },
  ];

  const settingsItem = { href: '/settings', label: 'Settings', icon: Settings };

  function isActive(pathname: string, href: string): boolean {
    if (href === '/') {
      return pathname === '/';
    }
    return pathname === href || pathname.startsWith(href + '/');
  }
</script>

<div class="flex flex-col gap-1">
  <!-- Platform Section -->
  <div class="mb-1">
    <p class="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-[0.15em] text-sidebar-foreground/30 font-mono select-none">
      Platform
    </p>
    <nav class="space-y-0.5">
      {#each platformItems as item}
        {@const active = isActive($page.url.pathname, item.href)}
        <a
          href={item.href}
          onclick={onclose}
          class="group flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-150
            {active
              ? 'bg-sidebar-accent text-sidebar-foreground'
              : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <!-- Active indicator bar -->
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150
            {active ? 'bg-sidebar-foreground opacity-100' : 'opacity-0'}">
          </span>
          <item.icon
            class="size-4 shrink-0 transition-colors duration-150
              {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}"
          />
          <span class="truncate">{item.label}</span>
          {#if active}
            <span class="ml-auto size-1.5 rounded-full bg-sidebar-foreground/60 shrink-0"></span>
          {/if}
        </a>
      {/each}
    </nav>
  </div>

  <!-- Divider -->
  <div class="my-1 border-t border-sidebar-border/50"></div>

  <!-- Operations Section -->
  <div class="mb-1">
    <p class="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-[0.15em] text-sidebar-foreground/30 font-mono select-none">
      Operations
    </p>
    <nav class="space-y-0.5">
      {#each operationsItems as item}
        {@const active = isActive($page.url.pathname, item.href)}
        <a
          href={item.href}
          onclick={onclose}
          class="group relative flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-150
            {active
              ? 'bg-sidebar-accent text-sidebar-foreground'
              : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150
            {active ? 'bg-sidebar-foreground opacity-100' : 'opacity-0'}">
          </span>
          <item.icon
            class="size-4 shrink-0 transition-colors duration-150
              {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}"
          />
          <span class="truncate">{item.label}</span>
          {#if active}
            <span class="ml-auto size-1.5 rounded-full bg-sidebar-foreground/60 shrink-0"></span>
          {/if}
        </a>
      {/each}
    </nav>
  </div>

  <!-- Divider -->
  <div class="my-1 border-t border-sidebar-border/50"></div>

  <!-- System Section -->
  {#each [settingsItem] as item}
    {@const active = isActive($page.url.pathname, item.href)}
    <div>
      <p class="px-3 py-1.5 text-[10px] font-semibold uppercase tracking-[0.15em] text-sidebar-foreground/30 font-mono select-none">
        System
      </p>
      <nav class="space-y-0.5">
        <a
          href={item.href}
          onclick={onclose}
          class="group relative flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-150
            {active
              ? 'bg-sidebar-accent text-sidebar-foreground'
              : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150
            {active ? 'bg-sidebar-foreground opacity-100' : 'opacity-0'}">
          </span>
          <item.icon
            class="size-4 shrink-0 transition-colors duration-150
              {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}"
          />
          <span class="truncate">{item.label}</span>
          {#if active}
            <span class="ml-auto size-1.5 rounded-full bg-sidebar-foreground/60 shrink-0"></span>
          {/if}
        </a>
      </nav>
    </div>
  {/each}
</div>
