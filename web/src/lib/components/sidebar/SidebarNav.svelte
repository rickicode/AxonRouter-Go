<script lang="ts">
  import { page } from '$app/stores';
  import * as Sidebar from '$lib/components/ui/sidebar';
  import {
    Home,
    Server,
    Layers,
    Terminal,
    Settings,
  } from '@lucide/svelte';

  let { onclose }: { onclose?: () => void } = $props();

  const primaryNavItems = [
    { href: '/', label: 'Home', icon: Home },
    { href: '/providers', label: 'Providers', icon: Server },
    { href: '/combos', label: 'Combos', icon: Layers },
    { href: '/logs', label: 'Logs', icon: Terminal },
  ];

  const settingsItem = { href: '/settings', label: 'Settings', icon: Settings };

  function isActive(pathname: string, href: string): boolean {
    if (href === '/') {
      return pathname === '/';
    }
    return pathname.startsWith(href);
  }
</script>

<div class="flex flex-col gap-5">
  <Sidebar.Group class="px-0 py-0">
    <Sidebar.GroupLabel class="h-7 px-2 text-[10px] font-semibold uppercase tracking-[0.18em] text-sidebar-foreground/40">
      Platform
    </Sidebar.GroupLabel>
    <Sidebar.GroupContent>
      <Sidebar.Menu class="gap-1.5">
        {#each primaryNavItems as item}
          {@const active = isActive($page.url.pathname, item.href)}
          <Sidebar.MenuItem>
            <Sidebar.MenuButton
              href={item.href}
              isActive={active}
              onclick={onclose}
              tooltip={item.label}
            >
              <item.icon />
              <span>{item.label}</span>
            </Sidebar.MenuButton>
          </Sidebar.MenuItem>
        {/each}

        {@const settingsActive = isActive($page.url.pathname, settingsItem.href)}
        <Sidebar.MenuItem>
          <Sidebar.MenuButton
            href={settingsItem.href}
            isActive={settingsActive}
            onclick={onclose}
            tooltip={settingsItem.label}
          >
            <settingsItem.icon />
            <span>{settingsItem.label}</span>
          </Sidebar.MenuButton>
        </Sidebar.MenuItem>
      </Sidebar.Menu>
    </Sidebar.GroupContent>
  </Sidebar.Group>
</div>

