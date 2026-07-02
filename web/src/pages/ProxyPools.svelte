<script lang="ts">
  import { onMount } from 'svelte';
  import { proxyPoolsApi, proxyGroupsApi, proxyDeployApi } from '$lib/api';
  import type { ProxyPool, ProxyGroup, DeployResult } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import * as Dialog from '$lib/components/ui/dialog';
  import { toast } from 'svelte-sonner';

  let tab = $state<'pools' | 'groups' | 'deploy'>('pools');
  let pools = $state<ProxyPool[]>([]);
  let groups = $state<ProxyGroup[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Modal state
  let showCreatePool = $state(false);
  let showCreateGroup = $state(false);

  // Create pool form
  let poolName = $state('');
  let poolUrl = $state('');
  let poolType = $state('http');
  let poolNoProxy = $state('');
  let createPoolLoading = $state(false);

  // Create group form
  let groupName = $state('');
  let groupMode = $state('roundrobin');
  let groupStickyLimit = $state(1);
  let groupStrict = $state(false);
  let createGroupLoading = $state(false);

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

  async function loadAll() {
    loading = true;
    error = '';
    try {
      const [poolsRes, groupsRes] = await Promise.all([
        proxyPoolsApi.list(),
        proxyGroupsApi.list(),
      ]);
      pools = poolsRes.data ?? [];
      groups = groupsRes.data ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load';
    } finally {
      loading = false;
    }
  }

  async function handleCreatePool() {
    if (!poolName.trim() || !poolUrl.trim()) return;
    createPoolLoading = true;
    try {
      await proxyPoolsApi.create({
        name: poolName.trim(),
        proxyUrl: poolUrl.trim(),
        type: poolType,
        noProxy: poolNoProxy.trim() || undefined,
        isActive: true,
      });
      toast.success('Proxy pool created');
      showCreatePool = false;
      poolName = '';
      poolUrl = '';
      poolNoProxy = '';
      await loadAll();
    } catch (err) {
      toast.error('Create failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      createPoolLoading = false;
    }
  }

  async function handleCreateGroup() {
    if (!groupName.trim()) return;
    createGroupLoading = true;
    try {
      await proxyGroupsApi.create({
        name: groupName.trim(),
        mode: groupMode,
        stickyLimit: groupStickyLimit,
        strictProxy: groupStrict,
        proxyPoolIds: [],
        isActive: true,
      });
      toast.success('Proxy group created');
      showCreateGroup = false;
      groupName = '';
      await loadAll();
    } catch (err) {
      toast.error('Create failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      createGroupLoading = false;
    }
  }

  async function testPool(id: string) {
    try {
      const res = await proxyPoolsApi.test(id);
      if (res.ok) {
        toast.success(`Proxy OK (${res.elapsedMs}ms)`);
      } else {
        toast.error(`Proxy failed: ${res.error || 'unknown'}`);
      }
      await loadAll();
    } catch (err) {
      toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function deletePool(id: string) {
    try {
      await proxyPoolsApi.delete(id);
      toast.success('Proxy pool deleted');
      await loadAll();
    } catch (err) {
      toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function deleteGroup(id: string) {
    try {
      await proxyGroupsApi.delete(id);
      toast.success('Proxy group deleted');
      await loadAll();
    } catch (err) {
      toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function togglePoolActive(pool: ProxyPool) {
    try {
      await proxyPoolsApi.update(pool.id, { isActive: !pool.isActive });
      toast.success(pool.isActive ? 'Pool disabled' : 'Pool enabled');
      await loadAll();
    } catch (err) {
      toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function toggleGroupActive(group: ProxyGroup) {
    try {
      await proxyGroupsApi.update(group.id, { isActive: !group.isActive });
      toast.success(group.isActive ? 'Group disabled' : 'Group enabled');
      await loadAll();
    } catch (err) {
      toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function runHealthCheck() {
    try {
      const res = await proxyPoolsApi.healthRun();
      toast.success(`Health check done (${res.results?.length ?? 0} pools)`);
      await loadAll();
    } catch (err) {
      toast.error('Health check failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
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
    deployLoading = true;
    deployResult = null;
    try {
      let res: DeployResult;
      if (deployPlatform === 'vercel') {
        res = await proxyDeployApi.vercel({ vercelToken: deployToken.trim(), projectName: deployProjectName.trim() || undefined });
      } else if (deployPlatform === 'deno') {
        if (!deployOrgDomain.trim()) { toast.error('Organization domain is required'); deployLoading = false; return; }
        res = await proxyDeployApi.deno({ denoToken: deployToken.trim(), orgDomain: deployOrgDomain.trim(), projectName: deployProjectName.trim() || undefined });
      } else {
        if (!deployAccountId.trim()) { toast.error('Account ID is required'); deployLoading = false; return; }
        res = await proxyDeployApi.cloudflare({ cfToken: deployToken.trim(), accountId: deployAccountId.trim(), projectName: deployProjectName.trim() || undefined });
      }
      deployResult = res;
      if (res.relayTest.ok) {
        toast.success(`Deployed to ${deployPlatform}! ${res.deployUrl}`);
      } else {
        toast.error(`Deployed but test failed: ${res.relayTest.error}`);
      }
      await loadAll();
    } catch (err) {
      toast.error('Deploy failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      deployLoading = false;
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
          <Button onclick={() => (showCreatePool = true)} class="text-button-md rounded-pill px-5">
            Add pool
          </Button>
        {:else if tab === 'groups'}
          <Button onclick={() => (showCreateGroup = true)} class="text-button-md rounded-pill px-5">
            Add group
          </Button>
        {/if}
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 border-b border-white/10">
      <button
        class="cursor-pointer px-4 py-2 text-body-sm transition-colors {tab === 'pools' ? 'border-b-2 border-foreground text-foreground' : 'text-muted-foreground hover:text-foreground'}"
        onclick={() => (tab = 'pools')}
      >
        Pools ({pools.length})
      </button>
      <button
        class="cursor-pointer px-4 py-2 text-body-sm transition-colors {tab === 'groups' ? 'border-b-2 border-foreground text-foreground' : 'text-muted-foreground hover:text-foreground'}"
        onclick={() => (tab = 'groups')}
      >
        Groups ({groups.length})
      </button>
      <button
        class="cursor-pointer px-4 py-2 text-body-sm transition-colors {tab === 'deploy' ? 'border-b-2 border-foreground text-foreground' : 'text-muted-foreground hover:text-foreground'}"
        onclick={() => (tab = 'deploy')}
      >
        Deploy
      </button>
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
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-caption-mono text-muted-foreground">{typeLabel(pool.type)}</span>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    <!-- State: user-controlled enabled/disabled -->
                    <button
                      onclick={() => togglePoolActive(pool)}
                      class="cursor-pointer inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase transition-colors
                        {pool.isActive
                          ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25'
                          : 'bg-zinc-500/15 text-zinc-500 border border-zinc-500/20 hover:bg-zinc-500/25'}"
                    >
                      <span class="size-1.5 rounded-full {pool.isActive ? 'bg-emerald-400' : 'bg-zinc-600'}"></span>
                      {pool.isActive ? 'On' : 'Off'}
                    </button>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    <!-- Health: last test result -->
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
      {:else}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-16">
            <div class="size-12 bg-muted rounded-md flex items-center justify-center mb-4">
              <svg class="size-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9" />
              </svg>
            </div>
            <h3 class="text-body-md-strong mb-1">No proxy pools configured.</h3>
            <p class="text-body-sm text-muted-foreground mb-4">
              Add an HTTP proxy or relay to route traffic through external endpoints.
            </p>
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
                    <a href="/proxy-pools/{group.id}" class="text-body-sm-strong hover:underline truncate block max-w-[160px]">{group.name}</a>
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
                    <button
                      onclick={() => toggleGroupActive(group)}
                      class="cursor-pointer inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase transition-colors
                        {group.isActive
                          ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25'
                          : 'bg-zinc-500/15 text-zinc-500 border border-zinc-500/20 hover:bg-zinc-500/25'}"
                    >
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
                    <Button onclick={() => deleteGroup(group.id)} variant="ghost" size="sm" class="text-caption-mono text-destructive h-6 px-2 rounded-sm">Del</Button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </Card>
      {:else}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-16">
            <div class="size-12 bg-muted rounded-md flex items-center justify-center mb-4">
              <svg class="size-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
              </svg>
            </div>
            <h3 class="text-body-md-strong mb-1">No proxy groups configured.</h3>
            <p class="text-body-sm text-muted-foreground mb-4">
              Group pools together with round-robin or sticky routing.
            </p>
            <Button onclick={() => (showCreateGroup = true)} class="text-button-md rounded-pill px-5">Add group</Button>
          </CardContent>
        </Card>
      {/if}
    {/if}

    <!-- Deploy Tab -->
    {#if tab === 'deploy'}
      <Card class="shadow-card">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md-strong">Deploy Relay Edge Function</CardTitle>
          <p class="text-body-sm text-muted-foreground">Auto-deploy a relay proxy to Vercel, Deno Deploy, or Cloudflare Workers.</p>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-2">
            <Label class="text-body-sm-strong">Platform</Label>
            <div class="flex gap-2">
              {#each (['vercel', 'deno', 'cloudflare'] as const) as p}
                <button
                  class="cursor-pointer px-4 py-2 rounded-sm text-body-sm border transition-colors {deployPlatform === p ? 'bg-foreground text-background border-foreground' : 'border-white/8 text-muted-foreground hover:text-foreground'}"
                  onclick={() => (deployPlatform = p)}
                >
                  {p === 'vercel' ? 'Vercel' : p === 'deno' ? 'Deno Deploy' : 'Cloudflare'}
                </button>
              {/each}
            </div>
          </div>
          <div class="space-y-2">
            <Label class="text-body-sm-strong">
              {deployPlatform === 'vercel' ? 'Vercel Token' : deployPlatform === 'deno' ? 'Deno Token' : 'Cloudflare API Token'}
            </Label>
            <Input bind:value={deployToken} type="password" placeholder="pat_xxx or API token" class="h-10 text-body-sm font-mono" />
          </div>
          {#if deployPlatform === 'deno'}
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Organization Domain</Label>
              <Input bind:value={deployOrgDomain} placeholder="your-org" class="h-10 text-body-sm font-mono" />
            </div>
          {/if}
          {#if deployPlatform === 'cloudflare'}
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Account ID</Label>
              <Input bind:value={deployAccountId} placeholder="abcdef1234567890" class="h-10 text-body-sm font-mono" />
            </div>
          {/if}
          <div class="space-y-2">
            <Label class="text-body-sm-strong">Project Name (optional)</Label>
            <Input bind:value={deployProjectName} placeholder="auto-generated if empty" class="h-10 text-body-sm" />
          </div>
          <Button onclick={handleDeploy} disabled={deployLoading || !deployToken.trim()} class="text-button-md rounded-pill px-5">
            {deployLoading ? 'Deploying...' : `Deploy to ${deployPlatform === 'vercel' ? 'Vercel' : deployPlatform === 'deno' ? 'Deno' : 'Cloudflare'}`}
          </Button>
          {#if deployResult}
            <Card class="shadow-card border {deployResult.relayTest.ok ? 'border-green-500/30' : 'border-destructive/30'}">
              <CardContent class="pt-4 space-y-2">
                <div class="flex items-center gap-2">
                  <Badge variant={deployResult.relayTest.ok ? 'default' : 'destructive'} class="text-caption-mono rounded-sm">
                    {deployResult.relayTest.ok ? 'Deployed' : 'Test Failed'}
                  </Badge>
                </div>
                <div class="space-y-1">
                  <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Deploy URL</p>
                  <p class="text-code font-mono break-all">{deployResult.deployUrl}</p>
                </div>
                <div class="space-y-1">
                  <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Relay Auth</p>
                  <p class="text-code font-mono break-all">{deployResult.relayAuth}</p>
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
    </Dialog.Header>
    <div class="space-y-4">
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Name</Label>
        <Input bind:value={poolName} placeholder="e.g. us-east-proxy, vercel-relay" class="h-10 text-body-sm" />
      </div>
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Proxy URL</Label>
        <Input bind:value={poolUrl} placeholder="http://proxy:8080 or https://relay.vercel.app" class="h-10 text-body-sm font-mono" />
      </div>
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Type</Label>
        <div class="flex gap-2">
          {#each typeOptions as opt}
            <button
              class="cursor-pointer px-3 py-1.5 rounded-sm text-body-sm border transition-colors {poolType === opt ? 'bg-foreground text-background border-foreground' : 'border-white/8 text-muted-foreground hover:text-foreground'}"
              onclick={() => (poolType = opt)}
            >
              {typeLabel(opt)}
            </button>
          {/each}
        </div>
      </div>
      <div class="space-y-2">
        <Label class="text-body-sm-strong">No Proxy (optional)</Label>
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
    </Dialog.Header>
    <div class="space-y-4">
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Name</Label>
        <Input bind:value={groupName} placeholder="e.g. us-proxies, failover-group" class="h-10 text-body-sm" />
      </div>
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Mode</Label>
        <div class="flex gap-2">
          <button
            class="cursor-pointer px-4 py-2 rounded-sm text-body-sm border transition-colors {groupMode === 'roundrobin' ? 'bg-foreground text-background border-foreground' : 'border-white/8 text-muted-foreground hover:text-foreground'}"
            onclick={() => (groupMode = 'roundrobin')}
          >
            Round Robin
          </button>
          <button
            class="cursor-pointer px-4 py-2 rounded-sm text-body-sm border transition-colors {groupMode === 'sticky' ? 'bg-foreground text-background border-foreground' : 'border-white/8 text-muted-foreground hover:text-foreground'}"
            onclick={() => (groupMode = 'sticky')}
          >
            Sticky
          </button>
        </div>
      </div>
      {#if groupMode === 'sticky'}
        <div class="space-y-2">
          <Label class="text-body-sm-strong">Sticky Limit</Label>
          <Input type="number" bind:value={groupStickyLimit} min={1} class="h-10 text-code font-mono" />
        </div>
      {/if}
      <div class="flex items-center gap-2">
        <input type="checkbox" bind:checked={groupStrict} class="rounded cursor-pointer" />
        <Label class="text-body-sm-strong cursor-pointer">Strict proxy</Label>
      </div>
      <p class="text-caption-mono text-muted-foreground">Add pools to this group after creation.</p>
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => (showCreateGroup = false)}>Cancel</Button>
      <Button onclick={handleCreateGroup} disabled={createGroupLoading || !groupName.trim()}>
        {createGroupLoading ? 'Creating...' : 'Create group'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
