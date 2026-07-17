<script lang="ts">
  import { currentPath, router } from '$lib/router';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import HomeIcon from '@lucide/svelte/icons/home';
  import ServerIcon from '@lucide/svelte/icons/server';
  import LayersIcon from '@lucide/svelte/icons/layers';
  import TerminalIcon from '@lucide/svelte/icons/terminal';
  import SettingsIcon from '@lucide/svelte/icons/settings';
  import GaugeIcon from '@lucide/svelte/icons/gauge';
  import GlobeIcon from '@lucide/svelte/icons/globe';
  import KeyIcon from '@lucide/svelte/icons/key';
  import ZapIcon from '@lucide/svelte/icons/zap';
  import BotIcon from '@lucide/svelte/icons/bot';
import BadgeDollarSignIcon from '@lucide/svelte/icons/badge-dollar-sign';
import ArchiveRestoreIcon from '@lucide/svelte/icons/archive-restore';
import BarChartIcon from '@lucide/svelte/icons/bar-chart';
  import CodeIcon from '@lucide/svelte/icons/code';
import InfoIcon from '@lucide/svelte/icons/info';

  let { onclose }: { onclose?: () => void } = $props();

const platformItems = [
 { href: '/', label: 'Dashboard', icon: HomeIcon },
 { href: '/providers', label: 'Providers', icon: ServerIcon },
 { href: '/combos', label: 'Combos', icon: LayersIcon },
 { href: '/usage', label: 'Usage', icon: BarChartIcon },
 { href: '/quota', label: 'Quota', icon: GaugeIcon },
 { href: '/optimization', label: 'Optimization', icon: ZapIcon },
 { href: '/logs', label: 'Logs', icon: TerminalIcon },
];
const systemItems = [
    { href: '/proxy-pools', label: 'Proxy Pools', icon: GlobeIcon },
 { href: '/api-keys', label: 'API Keys', icon: KeyIcon },
 { href: '/developers', label: 'Developers', icon: CodeIcon },
    { href: '/cli-tools', label: 'CLI Tools', icon: BotIcon },
{ href: '/model-pricing', label: 'Model Pricing', icon: BadgeDollarSignIcon },
{ href: '/backup-restore', label: 'Backup & Restore', icon: ArchiveRestoreIcon },
{ href: '/settings', label: 'Settings', icon: SettingsIcon },
  { href: '/about', label: 'About', icon: InfoIcon },
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
          class="group relative flex items-center gap-3 rounded-md px-3 py-2 text-body-sm-strong transition-all duration-150 {active ? 'bg-sidebar-accent text-sidebar-foreground' : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150 {active ? 'bg-sidebar-primary opacity-100' : 'opacity-0'}"></span>
          <item.icon class="size-4 shrink-0 transition-colors duration-150 {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}" />
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
          class="group relative flex items-center gap-3 rounded-md px-3 py-2 text-body-sm-strong transition-all duration-150 {active ? 'bg-sidebar-accent text-sidebar-foreground' : 'text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/60'}"
          aria-current={active ? 'page' : undefined}
        >
          <span class="absolute left-0 inset-y-0 w-0.5 rounded-r-full transition-all duration-150 {active ? 'bg-sidebar-primary opacity-100' : 'opacity-0'}"></span>
          <item.icon class="size-4 shrink-0 transition-colors duration-150 {active ? 'text-sidebar-foreground' : 'text-sidebar-foreground/40 group-hover:text-sidebar-foreground/80'}" />
          <span class="truncate">{item.label}</span>
          {#if active}
            <span class="ml-auto size-1.5 rounded-full bg-sidebar-primary shrink-0"></span>
          {/if}
        </a>
      {/each}
    </nav>
  </div>
</div>
