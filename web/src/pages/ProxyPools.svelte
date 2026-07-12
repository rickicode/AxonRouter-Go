<script lang="ts">
  import { onMount } from 'svelte';
  import { proxyPoolsApi, proxyGroupsApi, proxyDeployApi, providersApi, settingsApi } from '$lib/api';
  import type { ProxyPool, ProxyGroup, DeployResult, Provider } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Textarea } from '$lib/components/ui/textarea';
  import { Label } from '$lib/components/ui/label';
  import { Switch } from '$lib/components/ui/switch';
  import * as Select from '$lib/components/ui/select';
  import * as Dialog from '$lib/components/ui/dialog';
import Pagination from '$lib/components/Pagination.svelte';
  import { toast } from 'svelte-sonner';

  let tab = $state<'pools' | 'groups' | 'assignments' | 'deploy'>('pools');
  let pools = $state<ProxyPool[]>([]);
  let groups = $state<ProxyGroup[]>([]);
  let providers = $state<Provider[]>([]);
  let loading = $state(true);
  let error = $state('');
// Pools pagination
let poolPage = $state(1);
let poolPerPage = $state(10);
let poolTotal = $state(0);
let poolTotalPages = $state(1);

  // Modal state
  let showCreatePool = $state(false);
  let showCreateGroup = $state(false);
  let showEditGroup = $state(false);
  let showBulkImport = $state(false);

  // Create pool form
  let poolName = $state('');
  let poolUrl = $state('');
  let poolType = $state('http');
  let poolNoProxy = $state('');
  let createPoolLoading = $state(false);

  // Bulk import form
  let bulkText = $state('');
  let bulkType = $state('http');
  let bulkNamePrefix = $state('bulk');
  let bulkNoProxy = $state('');
  let bulkActive = $state(true);
  let bulkLoading = $state(false);

  // Create/Edit group form
  let groupName = $state('');
  let groupMode = $state('roundrobin');
  let groupStickyLimit = $state(1);
  let groupStrict = $state(false);
  let groupPoolIds = $state<string[]>([]);
  let createGroupLoading = $state(false);
  let editGroupId = $state('');
  let editGroupLoading = $state(false);

  // Assignments state
  let proxyDefaults = $state<Record<string, Record<string, string>>>({});
  let proxySaving = $state(false);

  // Deploy state
  let deployPlatform = $state<'vercel' | 'deno' | 'cloudflare'>('vercel');
  let deployToken = $state('');
  let deployProjectName = $state('');
  let deployOrgDomain = $state('');
  let deployAccountId = $state('');
  let deployLoading = $state(false);
  let deployResult = $state<DeployResult | null>(null);
  const typeOptions = ['http', 'vercel', 'deno', 'cloudflare'];

  // Derived stats
  const enabledCount = $derived(pools.filter(p => p.isActive).length);
  const onlineCount = $derived(pools.filter(p => p.testStatus === 'active').length);
  const errorCount = $derived(pools.filter(p => p.testStatus === 'error').length);

  onMount(() => {
    document.title = 'Proxy Pools — AxonRouter';
    loadAll();
  });

async function loadPools() {
  try {
    const res = await proxyPoolsApi.list({ page: String(poolPage), per_page: String(poolPerPage) });
    pools = res.data ?? [];
    poolTotal = res.pagination?.total ?? 0;
    poolTotalPages = res.pagination?.total_pages ?? 1;
  } catch (err) {
    toast.error('Failed to load pools: ' + (err instanceof Error ? err.message : 'Unknown'));
  }
}

async function onPoolPageChange(p: number) {
  if (p === poolPage) return;
  poolPage = p;
  await loadPools();
}

async function onPerPageChange(p: number) {
  if (p === poolPerPage) return;
  poolPerPage = p;
  poolPage = 1;
  await loadPools();
}

async function loadAll(silent = false) {
  if (!silent) {
    loading = true;
    error = '';
  }
  try {
    const [groupsRes, provRes, settingsRes] = await Promise.all([
      proxyGroupsApi.list(),
      providersApi.list(),
      settingsApi.list().catch(() => ({})),
    ]);
    groups = groupsRes.data ?? [];
    providers = provRes.data ?? [];
    const settings = ('data' in settingsRes ? (settingsRes as any).data : settingsRes) as Record<string, string>;
    const raw = settings?.['provider_proxy_defaults'];
    if (raw) { try { proxyDefaults = JSON.parse(raw); } catch { proxyDefaults = {}; } delete proxyDefaults['oc']; }
    await loadPools();
  } catch (err) {
    error = err instanceof Error ? err.message : 'Failed to load';
  } finally {
    if (!silent) loading = false;
  }
}

  // --- Pool CRUD ---
  async function handleCreatePool() {
    if (!poolName.trim() || !poolUrl.trim()) return;
    createPoolLoading = true;
    try {
      await proxyPoolsApi.create({ name: poolName.trim(), proxyUrl: poolUrl.trim(), type: poolType, noProxy: poolNoProxy.trim() || undefined, isActive: true });
      toast.success('Proxy pool created');
      showCreatePool = false;
      poolName = ''; poolUrl = ''; poolNoProxy = '';
  poolPage = 1;
  await loadAll(true);
    } catch (err) { toast.error('Create failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { createPoolLoading = false; }
  }

  function shortOrg(org?: string) {
    if (!org) return '';
    return org.replace(/^AS\d+\s*/, '').trim();
  }

  async function testPool(id: string) {
    try {
      const res = await proxyPoolsApi.test(id);
      if (res.ok) {
        const parts = [`${res.country || 'Unknown'}`, shortOrg(res.org)].filter(Boolean);
        toast.success(`Proxy OK (${res.elapsedMs}ms)${parts.length ? ' — ' + parts.join(' • ') : ''}`);
      } else {
        toast.error(`Proxy failed: ${res.error || 'unknown'}`);
      }
 await loadAll(true);
    } catch (err) { toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
  }

async function deletePool(id: string) {
  try { await proxyPoolsApi.delete(id); toast.success('Proxy pool deleted'); poolPage = 1; await loadAll(true); }
 catch (err) { toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
}

async function togglePoolActive(pool: ProxyPool) {
 try { await proxyPoolsApi.update(pool.id, { isActive: !pool.isActive }); toast.success(pool.isActive ? 'Pool disabled' : 'Pool enabled'); await loadAll(true); }
 catch (err) { toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
}

  // --- Group CRUD ---
  async function handleCreateGroup() {
    if (!groupName.trim()) return;
    createGroupLoading = true;
    try {
      await proxyGroupsApi.create({ name: groupName.trim(), mode: groupMode, stickyLimit: groupStickyLimit, strictProxy: groupStrict, proxyPoolIds: groupPoolIds, isActive: true });
      toast.success('Proxy group created');
      showCreateGroup = false;
      groupName = ''; groupPoolIds = [];
 await loadAll(true);
    } catch (err) { toast.error('Create failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { createGroupLoading = false; }
  }

  function openEditGroup(group: ProxyGroup) {
    editGroupId = group.id;
    groupName = group.name;
    groupMode = group.mode;
    groupStickyLimit = group.stickyLimit ?? 1;
    groupStrict = group.strictProxy ?? false;
    groupPoolIds = group.proxyPoolIds ? [...group.proxyPoolIds] : [];
    showEditGroup = true;
  }

  async function handleEditGroup() {
    if (!editGroupId || !groupName.trim()) return;
    editGroupLoading = true;
    try {
      await proxyGroupsApi.update(editGroupId, {
        name: groupName.trim(),
        mode: groupMode,
        stickyLimit: groupStickyLimit,
        strictProxy: groupStrict,
        proxyPoolIds: groupPoolIds,
      });
      toast.success('Group updated');
      showEditGroup = false;
 await loadAll(true);
    } catch (err) { toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { editGroupLoading = false; }
  }

  function toggleGroupPool(poolId: string) {
    if (groupPoolIds.includes(poolId)) {
      groupPoolIds = groupPoolIds.filter(id => id !== poolId);
    } else {
      groupPoolIds = [...groupPoolIds, poolId];
    }
  }

async function deleteGroup(id: string) {
 try { await proxyGroupsApi.delete(id); toast.success('Proxy group deleted'); await loadAll(true); }
 catch (err) { toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
}

async function toggleGroupActive(group: ProxyGroup) {
 try { await proxyGroupsApi.update(group.id, { isActive: !group.isActive }); toast.success(group.isActive ? 'Group disabled' : 'Group enabled'); await loadAll(true); }
 catch (err) { toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
}

  // --- Assignments ---
  function setProxyDefault(providerId: string, field: 'proxyPoolId' | 'proxyGroupId', value: string) {
    if (!proxyDefaults[providerId]) proxyDefaults[providerId] = {};
    if (value) proxyDefaults[providerId][field] = value;
    else delete proxyDefaults[providerId][field];
    if (Object.keys(proxyDefaults[providerId]).length === 0) delete proxyDefaults[providerId];
    proxyDefaults = { ...proxyDefaults };
  }

async function saveProxyDefaults() {
    proxySaving = true;
    try {
        const defaultsToSave = { ...proxyDefaults };
        delete defaultsToSave['oc'];
        await settingsApi.update('provider_proxy_defaults', JSON.stringify(defaultsToSave));
        toast.success('Proxy assignments saved');
    } catch (err) { toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { proxySaving = false; }
  }

  // --- Health & Deploy ---
  async function runHealthCheck() {
    try {
      const res = await proxyPoolsApi.healthRun();
      toast.success(`Health check done (${res.results?.length ?? 0} pools)`);
 await loadAll(true);
    } catch (err) { toast.error('Health check failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
  }

  function typeLabel(type: string): string {
    if (type === 'http') return 'HTTP';
    if (type === 'vercel') return 'Vercel';
    if (type === 'deno') return 'Deno';
    if (type === 'cloudflare') return 'CF';
    return type;
  }

  async function handleDeploy() {
    if (!deployToken.trim()) return;
    deployLoading = true; deployResult = null;
    try {
      let res: DeployResult;
      if (deployPlatform === 'vercel') res = await proxyDeployApi.vercel({ vercelToken: deployToken.trim(), projectName: deployProjectName.trim() || undefined });
      else if (deployPlatform === 'deno') {
        if (!deployOrgDomain.trim()) { toast.error('Organization domain is required'); deployLoading = false; return; }
        res = await proxyDeployApi.deno({ denoToken: deployToken.trim(), orgDomain: deployOrgDomain.trim(), projectName: deployProjectName.trim() || undefined });
      } else {
        if (!deployAccountId.trim()) { toast.error('Account ID is required'); deployLoading = false; return; }
        res = await proxyDeployApi.cloudflare({ cfToken: deployToken.trim(), accountId: deployAccountId.trim(), projectName: deployProjectName.trim() || undefined });
      }
	deployResult = res;
			if (res.relayTest.ok) {
				const parts = [`${res.relayTest.country || 'Unknown'}`, shortOrg(res.relayTest.org)].filter(Boolean);
				toast.success(`Deployed to ${deployPlatform}! ${parts.length ? parts.join(' • ') : res.deployUrl}`);
			} else {
				toast.error(`Deployed but test failed: ${res.relayTest.error}`);
			}
 await loadAll(true);
    } catch (err) { toast.error('Deploy failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { deployLoading = false; }
  }

  async function handleBulkImport() {
    const raw = bulkText.trim();
    if (!raw) return;
    bulkLoading = true;
    try {
      const items = raw
        .split('\n')
        .map(line => line.trim())
        .filter(line => line.length > 0 && !line.startsWith('#'));
      const res = await proxyPoolsApi.bulkCreate({
        items,
        defaultType: bulkType,
        namePrefix: bulkNamePrefix.trim() || 'bulk',
        noProxy: bulkNoProxy.trim() || undefined,
        isActive: bulkActive,
      });
      const msg = `${res.created} created, ${res.skipped} skipped, ${res.errors} errors`;
      if (res.errors === 0) {
        toast.success('Bulk import complete', { description: msg });
        showBulkImport = false;
      } else {
        toast.error('Bulk import finished with errors', { description: msg });
      }
      bulkText = '';
  poolPage = 1;
  await loadAll(true);
    } catch (err) {
      toast.error('Bulk import failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      bulkLoading = false;
    }
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if loading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="h-48 bg-muted animate-pulse rounded-md"></div>
    </div>
  {:else if error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{error}</p>
        <Button onclick={loadAll} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else}
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Proxy Pools.</h1>
        <div class="flex items-center gap-3 text-body-sm text-muted-foreground">
          <span>{pools.length} pools</span>
          <span class="text-border">·</span>
          <span class="inline-flex items-center gap-1">
            <span class="size-1.5 rounded-full bg-emerald-400"></span>
            {enabledCount} enabled
          </span>
          <span class="text-border">·</span>
          <span class="inline-flex items-center gap-1">
            <span class="size-1.5 rounded-full bg-sky-400"></span>
            {onlineCount} online
          </span>
          {#if errorCount > 0}
            <span class="text-border">·</span>
            <span class="inline-flex items-center gap-1 text-red-400">
              <span class="size-1.5 rounded-full bg-red-400"></span>
              {errorCount} error
            </span>
          {/if}
        </div>
      </div>
      <div class="flex gap-2">
        <Button onclick={runHealthCheck} variant="outline" class="text-body-sm rounded-pill px-4">
          Health check
        </Button>
        {#if tab === 'pools'}
          <Button onclick={() => (showCreatePool = true)} class="text-button-md rounded-pill px-5">Add pool</Button>
          <Button onclick={() => { bulkText = ''; bulkType = 'http'; bulkNamePrefix = 'bulk'; bulkNoProxy = ''; bulkActive = true; showBulkImport = true; }} variant="outline" class="text-button-md rounded-pill px-5">Bulk import</Button>
        {:else if tab === 'groups'}
          <Button onclick={() => { groupName = ''; groupMode = 'roundrobin'; groupStickyLimit = 1; groupStrict = false; groupPoolIds = []; showCreateGroup = true; }} class="text-button-md rounded-pill px-5">Add group</Button>
        {/if}
      </div>
    </div>

<!-- Tabs -->
		<div class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
      {#each ([['pools', `Pools (${pools.length})`], ['groups', `Groups (${groups.length})`], ['assignments', 'Assignments'], ['deploy', 'Deploy']] as const) as [key, label]}
        <button
          class="cursor-pointer rounded-md px-4 py-1.5 text-sm font-medium transition-all {tab === key ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
          onclick={() => (tab = key)}
        >{label}</button>
      {/each}
    </div>

    <!-- Pool Table -->
    {#if tab === 'pools'}
      {#if pools.length > 0}
        <Card class="shadow-card overflow-hidden p-0">
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-white/5 bg-white/[0.02]">
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Name</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Proxy URL</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Type</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">State</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Health</th>
                <th class="text-right text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Latency</th>
                <th class="text-right text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody>
              {#each pools as pool}
                <tr class="border-b border-white/[0.03] hover:bg-white/[0.02] transition-colors">
                  <td class="px-4 py-2.5">
                    <a href="/proxy-pools/{pool.id}" class="text-body-sm-strong hover:underline truncate block max-w-[160px]">{pool.name}</a>
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-caption-mono text-muted-foreground truncate block max-w-[220px]">{pool.proxyUrl}</span>
                    {#if pool.proxyCountry || pool.proxyIp}
                      <span class="text-[10px] text-muted-foreground/60 truncate block max-w-[220px]" title={pool.proxyIp || ''}>
                        {pool.proxyCountry || '—'}{pool.proxyCity ? ', ' + pool.proxyCity : ''}{pool.proxyOrg ? ' • ' + pool.proxyOrg.replace(/^AS\d+\s*/, '') : ''}
                      </span>
                    {/if}
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-caption-mono text-muted-foreground">{typeLabel(pool.type)}</span>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    <button onclick={() => togglePoolActive(pool)}
                      class="cursor-pointer inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase transition-colors
                        {pool.isActive ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25' : 'bg-zinc-500/15 text-zinc-500 border border-zinc-500/20 hover:bg-zinc-500/25'}">
                      <span class="size-1.5 rounded-full {pool.isActive ? 'bg-emerald-400' : 'bg-zinc-600'}"></span>
                      {pool.isActive ? 'On' : 'Off'}
                    </button>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    {#if pool.testStatus === 'active'}
                      <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase bg-sky-500/15 text-sky-400 border border-sky-500/30">
                        <span class="size-1.5 rounded-full bg-sky-400 animate-pulse"></span>
                        Online
                      </span>
                    {:else if pool.testStatus === 'error'}
                      <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase bg-red-500/15 text-red-400 border border-red-500/30" title={pool.lastError || ''}>
                        <span class="size-1.5 rounded-full bg-red-400"></span>
                        Error
                      </span>
                    {:else}
                      <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase bg-zinc-500/10 text-zinc-500 border border-zinc-500/20">
                        <span class="size-1.5 rounded-full bg-zinc-600"></span>
                        —
                      </span>
                    {/if}
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <span class="text-caption-mono {pool.responseTimeMs != null && pool.responseTimeMs < 500 ? 'text-emerald-400' : pool.responseTimeMs != null && pool.responseTimeMs < 2000 ? 'text-yellow-400' : 'text-muted-foreground'}">
                      {pool.responseTimeMs != null ? pool.responseTimeMs + 'ms' : '—'}
                    </span>
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <div class="flex gap-1 justify-end">
                      <Button onclick={() => testPool(pool.id)} variant="ghost" size="sm" class="text-caption-mono h-6 px-2 rounded-sm">Test</Button>
                      <Button onclick={() => deletePool(pool.id)} variant="ghost" size="sm" class="text-caption-mono text-destructive h-6 px-2 rounded-sm">Del</Button>
                    </div>
                  </td>
      </tr>
              {/each}
            </tbody>
          </table>
        </Card>

  <div class="mt-4">
    <Pagination page={poolPage} totalPages={poolTotalPages} total={poolTotal} perPage={poolPerPage} onPerPageChange={onPerPageChange} onChange={onPoolPageChange} />
  </div>
      {:else}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-16">
            <h3 class="text-body-md-strong mb-1">No proxy pools configured.</h3>
            <p class="text-body-sm text-muted-foreground mb-4">Add an HTTP proxy or relay to route traffic through external endpoints.</p>
            <Button onclick={() => (showCreatePool = true)} class="text-button-md rounded-pill px-5">Add pool</Button>
          </CardContent>
        </Card>
      {/if}
    {/if}

    <!-- Group Table -->
    {#if tab === 'groups'}
      {#if groups.length > 0}
        <Card class="shadow-card overflow-hidden p-0">
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-white/5 bg-white/[0.02]">
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Name</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Mode</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Pools</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">State</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Options</th>
                <th class="text-right text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody>
              {#each groups as group}
                <tr class="border-b border-white/[0.03] hover:bg-white/[0.02] transition-colors">
                  <td class="px-4 py-2.5">
                    <span class="text-body-sm-strong truncate block max-w-[160px]">{group.name}</span>
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-caption-mono text-muted-foreground capitalize">{group.mode}</span>
                    {#if group.mode === 'sticky'}
                      <span class="text-caption text-muted-foreground ml-1">({group.stickyLimit})</span>
                    {/if}
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    <span class="text-caption-mono">{group.proxyPoolIds?.length ?? 0}</span>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    <button onclick={() => toggleGroupActive(group)}
                      class="cursor-pointer inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase transition-colors
                        {group.isActive ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25' : 'bg-zinc-500/15 text-zinc-500 border border-zinc-500/20 hover:bg-zinc-500/25'}">
                      <span class="size-1.5 rounded-full {group.isActive ? 'bg-emerald-400' : 'bg-zinc-600'}"></span>
                      {group.isActive ? 'On' : 'Off'}
                    </button>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    {#if group.strictProxy}
                      <span class="text-[10px] uppercase tracking-wide text-yellow-400/80">Strict</span>
                    {:else}
                      <span class="text-muted-foreground">—</span>
                    {/if}
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <div class="flex gap-1 justify-end">
                      <Button onclick={() => openEditGroup(group)} variant="ghost" size="sm" class="text-caption-mono h-6 px-2 rounded-sm">Edit</Button>
                      <Button onclick={() => deleteGroup(group.id)} variant="ghost" size="sm" class="text-caption-mono text-destructive h-6 px-2 rounded-sm">Del</Button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </Card>
      {:else}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-16">
            <h3 class="text-body-md-strong mb-1">No proxy groups configured.</h3>
            <p class="text-body-sm text-muted-foreground mb-4">Group pools together with round-robin or sticky routing.</p>
            <Button onclick={() => { groupName = ''; groupMode = 'roundrobin'; groupStickyLimit = 1; groupStrict = false; groupPoolIds = []; showCreateGroup = true; }} class="text-button-md rounded-pill px-5">Add group</Button>
          </CardContent>
        </Card>
      {/if}
    {/if}

{#if tab === 'assignments'}
{#if providers.some(p => p.id !== 'oc') && pools.length > 0}
        <Card class="shadow-card overflow-hidden p-0">
          <div class="flex items-center justify-between px-4 py-3 border-b border-white/5 bg-white/[0.02]">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Provider → Proxy Assignment</p>
            <Button onclick={saveProxyDefaults} disabled={proxySaving} size="sm" class="text-body-sm rounded-sm">
              {proxySaving ? 'Saving...' : 'Save'}
            </Button>
          </div>
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-white/5">
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Provider</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Proxy Group</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Proxy Pool</th>
            </tr>
            </thead>
            <tbody>
              {#each providers.filter(p => p.id !== 'oc') as prov}
                <tr class="border-b border-white/[0.03] hover:bg-white/[0.02] transition-colors">
                  <td class="px-4 py-2.5">
                    <span class="text-body-sm-strong">{prov.display_name ?? prov.id}</span>
                    <span class="text-caption-mono text-muted-foreground ml-1">({prov.id})</span>
                  </td>
                  <td class="px-4 py-2.5">
                    <Select.Root type="single" value={proxyDefaults[prov.id]?.proxyGroupId ?? ''} onValueChange={(v: string) => setProxyDefault(prov.id, 'proxyGroupId', v ?? '')}>
                      <Select.Trigger class="h-8 w-full max-w-[200px] text-body-sm rounded-sm">
                        {groups.find(g => g.id === (proxyDefaults[prov.id]?.proxyGroupId ?? ''))?.name ?? 'None'}
                      </Select.Trigger>
                      <Select.Content>
                        <Select.Item value="" class="text-body-sm">None</Select.Item>
                        {#each groups as group}
                          <Select.Item value={group.id} class="text-body-sm">{group.name}</Select.Item>
                        {/each}
                      </Select.Content>
                    </Select.Root>
                  </td>
                  <td class="px-4 py-2.5">
                    <Select.Root type="single" value={proxyDefaults[prov.id]?.proxyPoolId ?? ''} onValueChange={(v: string) => setProxyDefault(prov.id, 'proxyPoolId', v ?? '')}>
                      <Select.Trigger class="h-8 w-full max-w-[200px] text-body-sm rounded-sm">
                        {pools.find(p => p.id === (proxyDefaults[prov.id]?.proxyPoolId ?? ''))?.name ?? 'None'}
                      </Select.Trigger>
                      <Select.Content>
                        <Select.Item value="" class="text-body-sm">None</Select.Item>
                        {#each pools as pool}
                          <Select.Item value={pool.id} class="text-body-sm">{pool.name}</Select.Item>
                        {/each}
                      </Select.Content>
                    </Select.Root>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </Card>
      {:else if providers.length === 0}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-16">
            <h3 class="text-body-md-strong mb-1">No providers found.</h3>
            <p class="text-body-sm text-muted-foreground">Add connections first to assign proxy pools.</p>
          </CardContent>
        </Card>
      {:else}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-16">
            <h3 class="text-body-md-strong mb-1">No proxy pools configured.</h3>
            <p class="text-body-sm text-muted-foreground">Create proxy pools first before assigning them to providers.</p>
          </CardContent>
        </Card>
      {/if}
    {/if}

<!-- Deploy Tab -->
	{#if tab === 'deploy'}
		<Card class="shadow-card">
			<CardHeader class="pb-3">
				<CardTitle class="text-base">Deploy Relay Edge Function</CardTitle>
				<CardDescription class="text-xs">
					Auto-deploy a relay proxy to Vercel, Deno Deploy, or Cloudflare Workers. The relay forwards upstream AI requests through your edge endpoint.
				</CardDescription>
			</CardHeader>
			<CardContent class="space-y-5">
				<div class="space-y-2">
					<Label class="text-sm font-medium">Platform</Label>
					<div class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
						{#each (['vercel', 'deno', 'cloudflare'] as const) as p}
							<button class="cursor-pointer rounded-md px-4 py-1.5 text-sm font-medium transition-all {deployPlatform === p ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (deployPlatform = p)}>
								{p === 'vercel' ? 'Vercel' : p === 'deno' ? 'Deno Deploy' : 'Cloudflare'}
							</button>
						{/each}
					</div>
				</div>
				<div class="space-y-2">
            <Label class="text-sm font-medium">{deployPlatform === 'vercel' ? 'Vercel Token' : deployPlatform === 'deno' ? 'Deno Token' : 'Cloudflare API Token'}</Label>
            <Input bind:value={deployToken} type="password" placeholder="pat_xxx or API token" class="h-10 text-body-sm font-mono" />
          </div>
          {#if deployPlatform === 'deno'}
            <div class="space-y-2">
              <Label class="text-sm font-medium">Organization Domain</Label>
              <Input bind:value={deployOrgDomain} placeholder="your-org" class="h-10 text-body-sm font-mono" />
            </div>
          {/if}
          {#if deployPlatform === 'cloudflare'}
            <div class="space-y-2">
              <Label class="text-sm font-medium">Account ID</Label>
              <Input bind:value={deployAccountId} placeholder="abcdef1234567890" class="h-10 text-body-sm font-mono" />
            </div>
          {/if}
          <div class="space-y-2">
            <Label class="text-sm font-medium">Project Name (optional)</Label>
            <Input bind:value={deployProjectName} placeholder="auto-generated if empty" class="h-10 text-body-sm" />
          </div>
				<div class="pt-1">
					<Button onclick={handleDeploy} disabled={deployLoading || !deployToken.trim()} class="text-button-md rounded-pill px-5">
						{deployLoading ? 'Deploying...' : `Deploy to ${deployPlatform === 'vercel' ? 'Vercel' : deployPlatform === 'deno' ? 'Deno' : 'Cloudflare'}`}
					</Button>
				</div>
			{#if deployResult}
				<Card class="shadow-card border {deployResult.relayTest.ok ? 'border-emerald-500/30' : 'border-destructive/30'}">
					<CardHeader class="pb-3">
						<div class="flex items-center justify-between">
							<CardTitle class="text-base">{deployResult.relayTest.ok ? 'Deployed' : 'Test Failed'}</CardTitle>
							<Badge variant={deployResult.relayTest.ok ? 'default' : 'destructive'} class="text-caption-mono rounded-sm">
								{deployResult.relayTest.ok ? 'Online' : 'Error'}
							</Badge>
						</div>
						{#if deployResult.relayTest.ok}
							<CardDescription class="text-xs">Relay is online and returning geo/ISP information.</CardDescription>
						{:else}
							<CardDescription class="text-xs">The relay was deployed but the health probe failed. Check the error below.</CardDescription>
						{/if}
					</CardHeader>
					<CardContent class="space-y-4 pt-0">
						{#if deployResult.relayTest.ok && (deployResult.relayTest.country || deployResult.relayTest.org)}
							<div class="grid grid-cols-3 gap-3 rounded-lg bg-muted p-3">
								<div class="space-y-1">
									<p class="text-caption-mono text-muted-foreground uppercase font-semibold">IP</p>
									<p class="text-code font-mono break-all">{deployResult.relayTest.ip ?? '-'}</p>
								</div>
								<div class="space-y-1">
									<p class="text-caption-mono text-muted-foreground uppercase font-semibold">Country</p>
									<p class="text-code font-mono">{deployResult.relayTest.country ?? '-'}</p>
								</div>
								<div class="space-y-1">
									<p class="text-caption-mono text-muted-foreground uppercase font-semibold">ISP</p>
									<p class="text-code font-mono break-all">{shortOrg(deployResult.relayTest.org) ?? '-'}</p>
								</div>
							</div>
						{/if}
						<div class="space-y-4 rounded-lg bg-muted p-3">
							<div class="space-y-1">
								<p class="text-caption-mono text-muted-foreground uppercase font-semibold">Deploy URL</p>
								<p class="text-code font-mono break-all">{deployResult.deployUrl}</p>
							</div>
							<div class="space-y-1">
								<p class="text-caption-mono text-muted-foreground uppercase font-semibold">Relay Auth</p>
								<p class="text-code font-mono break-all">{deployResult.relayAuth}</p>
							</div>
						</div>
						{#if !deployResult.relayTest.ok}
							<div class="space-y-1">
								<p class="text-caption-mono text-muted-foreground uppercase font-semibold">Test Error</p>
								<p class="text-body-sm font-mono text-destructive">{deployResult.relayTest.error}</p>
							</div>
						{/if}
					</CardContent>
				</Card>
			{/if}
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>

<!-- Create Pool Dialog -->
<Dialog.Root bind:open={showCreatePool}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">Create proxy pool</Dialog.Title>
      <Dialog.Description class="text-xs">Add an HTTP proxy or hosted relay to route provider traffic.</Dialog.Description>
    </Dialog.Header>
    <div class="space-y-4 py-2">
      <div class="space-y-2">
        <Label class="text-sm font-medium">Name</Label>
        <Input bind:value={poolName} placeholder="e.g. us-east-proxy" class="h-10 text-body-sm" />
      </div>
      <div class="space-y-2">
        <Label class="text-sm font-medium">Proxy URL</Label>
        <Input bind:value={poolUrl} placeholder="http://proxy:8080" class="h-10 text-body-sm font-mono" />
      </div>
      <div class="space-y-2">
        <Label class="text-sm font-medium">Type</Label>
        <div class="inline-flex w-fit flex-wrap items-center gap-1 rounded-lg bg-muted p-1">
          {#each typeOptions as opt}
            <button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {poolType === opt ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (poolType = opt)}>
              {typeLabel(opt)}
            </button>
          {/each}
        </div>
      </div>
      <div class="space-y-2">
        <Label class="text-sm font-medium">No Proxy (optional)</Label>
        <Input bind:value={poolNoProxy} placeholder="localhost,127.0.0.1" class="h-10 text-body-sm font-mono" />
      </div>
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => (showCreatePool = false)}>Cancel</Button>
      <Button onclick={handleCreatePool} disabled={createPoolLoading || !poolName.trim() || !poolUrl.trim()}>
        {createPoolLoading ? 'Creating...' : 'Create pool'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Create Group Dialog -->
<Dialog.Root bind:open={showCreateGroup}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">Create proxy group</Dialog.Title>
      <Dialog.Description class="text-xs">Combine multiple pools with round-robin or sticky routing.</Dialog.Description>
    </Dialog.Header>
    <div class="space-y-4">
      <div class="space-y-2">
        <Label class="text-sm font-medium">Name</Label>
        <Input bind:value={groupName} placeholder="e.g. us-proxies" class="h-10 text-body-sm" />
      </div>
      <div class="space-y-2">
        <Label class="text-sm font-medium">Mode</Label>
				<div class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
					<button class="cursor-pointer rounded-md px-4 py-1.5 text-sm font-medium transition-all {groupMode === 'roundrobin' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'roundrobin')}>Round Robin</button>
					<button class="cursor-pointer rounded-md px-4 py-1.5 text-sm font-medium transition-all {groupMode === 'sticky' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'sticky')}>Sticky</button>
				</div>
			</div>
      {#if groupMode === 'sticky'}
        <div class="space-y-2">
          <Label class="text-sm font-medium">Sticky Limit</Label>
          <Input type="number" bind:value={groupStickyLimit} min={1} class="h-10 text-code font-mono" />
        </div>
      {/if}
      <div class="flex items-center space-x-2">
        <Switch id="group-strict" bind:checked={groupStrict} />
        <Label for="group-strict" class="text-sm font-medium cursor-pointer">Strict proxy</Label>
      </div>
      {#if pools.length > 0}
        <div class="space-y-2">
          <Label class="text-sm font-medium">Pools</Label>
          <div class="flex flex-wrap gap-1.5">
            {#each pools as pool}
              <button class="cursor-pointer px-2.5 py-1 rounded-md text-caption-mono border transition-colors {groupPoolIds.includes(pool.id) ? 'bg-foreground text-background border-foreground' : 'border-white/10 text-muted-foreground hover:text-foreground'}" onclick={() => toggleGroupPool(pool.id)}>
                {pool.name}
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => (showCreateGroup = false)}>Cancel</Button>
      <Button onclick={handleCreateGroup} disabled={createGroupLoading || !groupName.trim()}>
        {createGroupLoading ? 'Creating...' : 'Create'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Edit Group Dialog -->
<Dialog.Root bind:open={showEditGroup}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">Edit proxy group</Dialog.Title>
      <Dialog.Description class="text-xs">Update routing mode, strict proxy, and pool membership.</Dialog.Description>
    </Dialog.Header>
    <div class="space-y-4">
      <div class="space-y-2">
        <Label class="text-sm font-medium">Name</Label>
        <Input bind:value={groupName} class="h-10 text-body-sm" />
      </div>
      <div class="space-y-2">
        <Label class="text-sm font-medium">Mode</Label>
				<div class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
					<button class="cursor-pointer rounded-md px-4 py-1.5 text-sm font-medium transition-all {groupMode === 'roundrobin' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'roundrobin')}>Round Robin</button>
					<button class="cursor-pointer rounded-md px-4 py-1.5 text-sm font-medium transition-all {groupMode === 'sticky' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'sticky')}>Sticky</button>
				</div>
			</div>
      {#if groupMode === 'sticky'}
        <div class="space-y-2">
          <Label class="text-sm font-medium">Sticky Limit</Label>
          <Input type="number" bind:value={groupStickyLimit} min={1} class="h-10 text-code font-mono" />
        </div>
      {/if}
      <div class="flex items-center space-x-2">
        <Switch id="group-strict" bind:checked={groupStrict} />
        <Label for="group-strict" class="text-sm font-medium cursor-pointer">Strict proxy</Label>
      </div>
      {#if pools.length > 0}
        <div class="space-y-2">
          <Label class="text-sm font-medium">Pools</Label>
          <div class="flex flex-wrap gap-1.5">
            {#each pools as pool}
              <button class="cursor-pointer px-2.5 py-1 rounded-md text-caption-mono border transition-colors {groupPoolIds.includes(pool.id) ? 'bg-foreground text-background border-foreground' : 'border-white/10 text-muted-foreground hover:text-foreground'}" onclick={() => toggleGroupPool(pool.id)}>
                {pool.name}
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => (showEditGroup = false)}>Cancel</Button>
	<Button onclick={handleEditGroup} disabled={editGroupLoading || !groupName.trim()}>
		{editGroupLoading ? 'Saving...' : 'Save'}
	</Button>
</Dialog.Footer>
</Dialog.Content>
</Dialog.Root>

<!-- Bulk Import Dialog -->
<Dialog.Root bind:open={showBulkImport}>
	<Dialog.Content class="sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title class="text-body-md-strong">Bulk import proxy pools</Dialog.Title>
			<Dialog.Description class="text-xs">
				Paste one proxy URL per line. Lines starting with # are ignored. Optionally use <code>name|url</code>.
			</Dialog.Description>
		</Dialog.Header>
		<div class="space-y-4 py-2">
			<div class="space-y-2">
				<Label class="text-sm font-medium">Proxy URLs</Label>
				<Textarea bind:value={bulkText} placeholder="http://user:pass@proxy:8080&#10;my-relay|https://relay.vercel.app" rows={8} class="text-body-sm font-mono" />
			</div>
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-2">
					<Label class="text-sm font-medium">Default type</Label>
					<div class="inline-flex w-fit flex-wrap items-center gap-1 rounded-lg bg-muted p-1">
						{#each typeOptions as opt}
							<button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {bulkType === opt ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (bulkType = opt)}>{typeLabel(opt)}</button>
						{/each}
					</div>
				</div>
				<div class="space-y-2">
					<Label class="text-sm font-medium">Name prefix</Label>
					<Input bind:value={bulkNamePrefix} placeholder="bulk" class="h-10 text-body-sm" />
				</div>
			</div>
			<div class="space-y-2">
				<Label class="text-sm font-medium">No Proxy (optional)</Label>
				<Input bind:value={bulkNoProxy} placeholder="localhost,127.0.0.1" class="h-10 text-body-sm font-mono" />
			</div>
			<div class="flex items-center gap-3">
				<Switch id="bulk-active" bind:checked={bulkActive} />
				<Label for="bulk-active" class="text-sm font-medium">Active after import</Label>
			</div>
		</div>
		<Dialog.Footer>
			<Button variant="ghost" onclick={() => (showBulkImport = false)}>Cancel</Button>
			<Button onclick={handleBulkImport} disabled={bulkLoading || !bulkText.trim()}>
				{bulkLoading ? 'Importing...' : 'Import pools'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
