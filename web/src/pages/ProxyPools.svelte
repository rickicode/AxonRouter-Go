<script lang="ts">
  import { onMount } from 'svelte';
  import { proxyPoolsApi, proxyGroupsApi } from '$lib/api';
  import type { ProxyPool, ProxyGroup } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import * as Dialog from '$lib/components/ui/dialog';
  import { toast } from 'svelte-sonner';
  let tab = $state<'pools' | 'groups'>('pools');
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

  const typeOptions = ['http', 'vercel', 'deno', 'cloudflare'];

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
      toast.success(pool.isActive ? 'Pool deactivated' : 'Pool activated');
      await loadAll();
    } catch (err) {
      toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function toggleGroupActive(group: ProxyGroup) {
    try {
      await proxyGroupsApi.update(group.id, { isActive: !group.isActive });
      toast.success(group.isActive ? 'Group deactivated' : 'Group activated');
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

  function statusColor(status: string): string {
    if (status === 'active') return 'default';
    if (status === 'error') return 'destructive';
    return 'secondary';
  }

  function typeLabel(type: string): string {
    if (type === 'http') return 'HTTP';
    if (type === 'vercel') return 'Vercel';
    if (type === 'deno') return 'Deno';
    if (type === 'cloudflare') return 'CF';
    return type;
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if loading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {#each Array(3) as _}
          <div class="h-40 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
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
        <p class="text-body-sm text-muted-foreground">
          {pools.length} pools, {groups.length} groups configured.
        </p>
      </div>
      <div class="flex gap-2">
        <Button onclick={runHealthCheck} variant="outline" class="text-body-sm rounded-pill px-4">
          Health check
        </Button>
        {#if tab === 'pools'}
          <Button onclick={() => (showCreatePool = true)} class="text-button-md rounded-pill px-5">
            Add pool
          </Button>
        {:else}
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
    </div>

    <!-- Pool List -->
    {#if tab === 'pools'}
      {#if pools.length > 0}
        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {#each pools as pool}
            <Card class="shadow-card transition-all hover:bg-accent/10 hover:border-foreground/20 h-full">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-2">
                <div class="space-y-1 min-w-0">
                  <CardTitle class="text-body-md-strong truncate">{pool.name}</CardTitle>
                  <p class="text-caption-mono text-muted-foreground truncate">{pool.proxyUrl}</p>
                </div>
                <div class="flex gap-1 flex-shrink-0">
                  <Badge variant={pool.isActive ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
                    {pool.isActive ? 'Active' : 'Off'}
                  </Badge>
                  <Badge variant={statusColor(pool.testStatus)} class="text-caption-mono rounded-sm">
                    {pool.testStatus}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent class="flex flex-col gap-3">
                <div class="grid grid-cols-2 gap-2 border-t border-white/5 pt-3">
                  <div>
                    <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Type</p>
                    <p class="text-body-sm mt-0.5">{typeLabel(pool.type)}</p>
                  </div>
                  <div>
                    <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Latency</p>
                    <p class="text-code font-mono mt-0.5">{pool.responseTimeMs != null ? pool.responseTimeMs + 'ms' : '—'}</p>
                  </div>
                </div>
                {#if pool.lastError}
                  <p class="text-caption-mono text-destructive truncate" title={pool.lastError}>{pool.lastError}</p>
                {/if}
                <div class="flex gap-2 pt-1">
                  <Button onclick={() => testPool(pool.id)} variant="outline" size="sm" class="text-caption-mono rounded-sm flex-1">Test</Button>
                  <Button onclick={() => togglePoolActive(pool)} variant="outline" size="sm" class="text-caption-mono rounded-sm">
                    {pool.isActive ? 'Disable' : 'Enable'}
                  </Button>
                  <Button onclick={() => deletePool(pool.id)} variant="ghost" size="sm" class="text-caption-mono text-destructive rounded-sm">Delete</Button>
                </div>
              </CardContent>
            </Card>
          {/each}
        </div>
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

    <!-- Group List -->
    {#if tab === 'groups'}
      {#if groups.length > 0}
        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {#each groups as group}
            <Card class="shadow-card transition-all hover:bg-accent/10 hover:border-foreground/20 h-full">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-2">
                <div class="space-y-1 min-w-0">
                  <CardTitle class="text-body-md-strong truncate">{group.name}</CardTitle>
                  <p class="text-caption-mono text-muted-foreground">{group.proxyPoolIds?.length ?? 0} pools</p>
                </div>
                <Badge variant={group.isActive ? 'default' : 'secondary'} class="text-caption-mono rounded-sm flex-shrink-0">
                  {group.isActive ? 'Active' : 'Off'}
                </Badge>
              </CardHeader>
              <CardContent class="flex flex-col gap-3">
                <div class="grid grid-cols-2 gap-2 border-t border-white/5 pt-3">
                  <div>
                    <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Mode</p>
                    <p class="text-body-sm mt-0.5">{group.mode}</p>
                  </div>
                  <div>
                    <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Sticky</p>
                    <p class="text-code font-mono mt-0.5">{group.mode === 'sticky' ? group.stickyLimit : '—'}</p>
                  </div>
                  {#if group.strictProxy}
                    <div class="col-span-2">
                      <Badge variant="outline" class="text-caption-mono rounded-sm">Strict proxy</Badge>
                    </div>
                  {/if}
                </div>
                <div class="flex gap-2 pt-1">
                  <Button onclick={() => toggleGroupActive(group)} variant="outline" size="sm" class="text-caption-mono rounded-sm flex-1">
                    {group.isActive ? 'Disable' : 'Enable'}
                  </Button>
                  <Button onclick={() => deleteGroup(group.id)} variant="ghost" size="sm" class="text-caption-mono text-destructive rounded-sm">Delete</Button>
                </div>
              </CardContent>
            </Card>
          {/each}
        </div>
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
              Group multiple pools with round-robin or sticky selection.
            </p>
            <Button onclick={() => (showCreateGroup = true)} class="text-button-md rounded-pill px-5">Add group</Button>
          </CardContent>
        </Card>
      {/if}
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
