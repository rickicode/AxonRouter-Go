<script lang="ts">
  import './app.css';
  import { onMount } from 'svelte';
  import { router, currentPath } from '$lib/router';
  import { startHealthChecks } from '$lib/health';
import * as Sidebar from '$lib/components/ui/sidebar';
  import { Toaster } from '$lib/components/ui/sonner';
  import { toast } from 'svelte-sonner';
  import SidebarNav from '$lib/components/sidebar/SidebarNav.svelte';
  import SidebarBrand from '$lib/components/sidebar/SidebarBrand.svelte';
  import SidebarHealth from '$lib/components/sidebar/SidebarHealth.svelte';
import { authStore, logout, mustChangePasswordStore, isPasswordWarningDismissed } from '$lib/auth';
import Login from './pages/Login.svelte';
import ChangePasswordModal from '$lib/components/ChangePasswordModal.svelte';
import UpdateAvailableModal from '$lib/components/UpdateAvailableModal.svelte';
import LogOutIcon from '@lucide/svelte/icons/log-out';
import HeartIcon from '@lucide/svelte/icons/heart';
import { Button } from '$lib/components/ui/button';

  // Page components
  import Dashboard from './pages/Dashboard.svelte';
  import Providers from './pages/Providers.svelte';
  import ProviderDetail from './pages/ProviderDetail.svelte';
  import ConnectionDetail from './pages/ConnectionDetail.svelte';
  import Combos from './pages/Combos.svelte';
  import ComboDetail from './pages/ComboDetail.svelte';
  import Logs from './pages/Logs.svelte';
  import Settings from './pages/Settings.svelte';
  import Quota from './pages/Quota.svelte';
  import ProxyPools from './pages/ProxyPools.svelte';
  import ProxyPoolDetail from './pages/ProxyPoolDetail.svelte';
  import APIKeys from './pages/APIKeys.svelte';
import Optimization from './pages/Optimization.svelte';
import CLITools from './pages/CLITools.svelte';
	import ModelPricing from './pages/ModelPricing.svelte';
	import Developers from './pages/Developers.svelte';
import Usage from './pages/Usage.svelte';
import BackupRestore from './pages/BackupRestore.svelte';
import Console from './pages/Console.svelte';
import About from './pages/About.svelte';
import NotFound from './pages/NotFound.svelte';

  let cleanup: (() => void) | undefined;
  let stopHealthChecks: (() => void) | undefined;

  onMount(() => {
    cleanup = router.start();
    stopHealthChecks = startHealthChecks();
    return () => {
      cleanup?.();
      stopHealthChecks?.();
    };
  });

  function getPageLabel(path: string): string {
    if (path === '/') return 'Dashboard';
    const segment = path.split('/').filter(Boolean)[0];
const labels: Record<string, string> = {
		providers: 'Providers',
		combos: 'Combos',
		logs: 'Logs',
		quota: 'Quota',
		settings: 'Settings',
		'proxy-pools': 'Proxy Pools',
		'cli-tools': 'CLI Tools',
		'model-pricing': 'Model Pricing',
'developers': 'Developers',
		'backup-restore': 'Backup & Restore',
		'console': 'Console',
		'about': 'About',
  };
    return labels[segment] ?? segment.charAt(0).toUpperCase() + segment.slice(1);
  }

  function matchRoute(path: string): { component: any; params: Record<string, string> } {
    const segments = path.split('/').filter(Boolean);

    // / → Dashboard
    if (segments.length === 0) return { component: Dashboard, params: {} };

    // /providers → Providers
    if (segments[0] === 'providers' && segments.length === 1) return { component: Providers, params: {} };


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

    // /quota → Quota
    if (segments[0] === 'quota' && segments.length === 1) return { component: Quota, params: {} };

    // /settings → Settings
    if (segments[0] === 'settings') return { component: Settings, params: {} };

    // /proxy-pools/:id → ProxyPoolDetail
    if (segments[0] === 'proxy-pools' && segments.length === 2) return { component: ProxyPoolDetail, params: { id: segments[1] } };

    // /proxy-pools → ProxyPools
    if (segments[0] === 'proxy-pools' && segments.length === 1) return { component: ProxyPools, params: {} };

    // /api-keys → APIKeys
    if (segments[0] === 'api-keys') return { component: APIKeys, params: {} };

  // /optimization → Optimization
  if (segments[0] === 'optimization' && segments.length === 1) return { component: Optimization, params: {} };

// /cli-tools → CLI Tools
  if (segments[0] === 'cli-tools' && segments.length === 1) return { component: CLITools, params: {} };

	// /usage → Usage
	if (segments[0] === 'usage' && segments.length === 1) return { component: Usage, params: {} };

  // /model-pricing → ModelPricing
  if (segments[0] === 'model-pricing' && segments.length === 1) return { component: ModelPricing, params: {} };

// /developers → Developers
if (segments[0] === 'developers' && segments.length === 1) return { component: Developers, params: {} };

// /backup-restore → BackupRestore
if (segments[0] === 'backup-restore' && segments.length === 1) return { component: BackupRestore, params: {} };

// /console → Console
if (segments[0] === 'console' && segments.length === 1) return { component: Console, params: {} };

// /about → About
if (segments[0] === 'about' && segments.length === 1) return { component: About, params: {} };

// Fallback → 404
 return { component: NotFound, params: {} };
  }

  let route = $derived(matchRoute($currentPath));

function handleLogout() {
  logout();
  toast.success('Signed out');
}
  let pageLabel = $derived(getPageLabel($currentPath));
</script>

<Toaster />

{#if $authStore}
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
      <h1 class="text-body-md-strong text-foreground">{pageLabel}</h1>
<div class="ml-auto flex items-center gap-2">
  <a href="https://saweria.co/HIJILABS" target="_blank" rel="noopener noreferrer" class="flex items-center gap-1.5 text-caption-mono text-muted-foreground hover:text-foreground transition-colors">
    <HeartIcon class="size-4" />
    <span class="hidden sm:inline">Support us</span>
  </a>
  <Button variant="ghost" size="sm" class="gap-1.5" onclick={handleLogout}>
    <LogOutIcon class="size-4" />
    <span class="hidden sm:inline">Logout</span>
  </Button>
</div>
    </header>

    <main class="flex-1 overflow-auto">
      <route.component {...route.params} />
    </main>
  </Sidebar.Inset>
  </Sidebar.Provider>

  {#if $mustChangePasswordStore && !isPasswordWarningDismissed()}
    <ChangePasswordModal />
  {/if}
  <UpdateAvailableModal />
{:else}
  <Login />
{/if}

