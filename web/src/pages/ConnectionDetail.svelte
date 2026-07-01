<script lang="ts">
  import { onMount } from 'svelte';
  import { loadConnection, selectedConnection, isLoading, error } from '$lib/stores';
  import { unwrapInt, unwrapStr } from '$lib/utils';
  import { connectionsApi, proxyPoolsApi, proxyGroupsApi } from '$lib/api';
  import type { ProxyPool, ProxyGroup } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { router } from '$lib/router';
  import { getProviderMeta, getStatusDotColor, getStatusVariant, getStatusLabel } from '$lib/provider-catalog';
  import { toast } from 'svelte-sonner';

  let { id = '', connId = '' }: { id?: string; connId?: string } = $props();
  let providerId = $derived(id);
  let connectionId = $derived(connId);
  let actionLoading = $state('');
  let editingName = $state(false);
  let pools = $state<ProxyPool[]>([]);
  let groups = $state<ProxyGroup[]>([]);
  let selectedPoolId = $state('');
  let selectedGroupId = $state('');
  let proxySaving = $state(false);
  let editName = $state('');

  onMount(async () => {
    document.title = 'Connection — AxonRouter';
    await loadConnection(connectionId);
    loadProxyData();
  });

  async function loadProxyData() {
    try {
      const [poolsRes, groupsRes] = await Promise.all([proxyPoolsApi.list(), proxyGroupsApi.list()]);
      pools = poolsRes.data ?? [];
      groups = groupsRes.data ?? [];
      // Read current assignments from provider_specific_data
      if ($selectedConnection?.provider_specific_data) {
        try {
          const psd = JSON.parse($selectedConnection.provider_specific_data);
          selectedPoolId = psd.proxyPoolId ?? '';
          selectedGroupId = psd.proxyGroupId ?? '';
        } catch { /* ignore */ }
      }
    } catch { /* ignore */ }
  }

  async function saveProxyAssignment() {
    if (!$selectedConnection) return;
    proxySaving = true;
    try {
      const psd: Record<string, string> = {};
      if (selectedGroupId) psd.proxyGroupId = selectedGroupId;
      if (selectedPoolId) psd.proxyPoolId = selectedPoolId;
      await connectionsApi.update(connectionId, { provider_specific_data: JSON.stringify(psd) });
      await loadConnection(connectionId);
      toast.success('Proxy assignment saved');
    } catch (err) {
      toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      proxySaving = false;
    }
  }
  function formatCooldown(raw: unknown): string {
    const cooldownUntil = unwrapInt(raw);
    if (!cooldownUntil) return 'None';
    const now = Math.floor(Date.now() / 1000);
    if (cooldownUntil <= now) return 'Expired';
    const seconds = cooldownUntil - now;
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  }

  function formatTimestamp(raw: unknown) {
    const timestamp = unwrapInt(raw);
    if (!timestamp) return 'Never';
    return new Date(timestamp * 1000).toLocaleString();
  }

  async function handleTest() {
    try { await connectionsApi.test(connectionId); await loadConnection(connectionId); toast.success('Connection test passed'); }
    catch (err) { toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { actionLoading = ''; }
  }

  async function handleReset() {
    actionLoading = 'reset';
    try { await connectionsApi.reset(connectionId); await loadConnection(connectionId); toast.success('Connection reset'); }
    catch (err) { toast.error('Reset failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { actionLoading = ''; }
  }

  async function handleToggle() {
    if (!$selectedConnection) return;
    const willBeActive = !$selectedConnection.is_active;
    actionLoading = 'toggle';
    try { await connectionsApi.update(connectionId, { is_active: willBeActive }); await loadConnection(connectionId); toast.success(willBeActive ? 'Connection enabled' : 'Connection disabled'); }
    catch (err) { toast.error('Toggle failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { actionLoading = ''; }
  }

  async function handleDelete() {
    if (!confirm('Delete this connection? This cannot be undone.')) return;
    actionLoading = 'delete';
    try { await connectionsApi.delete(connectionId); toast.success('Connection deleted'); router.navigate(`/providers/${providerId}`); }
    catch (err) { toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown')); actionLoading = ''; }
  }

  async function handleSaveName() {
    if (!$selectedConnection || !editName.trim()) return;
    try { await connectionsApi.update(connectionId, { name: editName.trim() }); editingName = false; await loadConnection(connectionId); toast.success('Name updated'); }
    catch (err) { toast.error('Rename failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
  }

  function startEditName() {
    if ($selectedConnection) { editName = $selectedConnection.name; editingName = true; }
  }

  let capabilities = $derived.by(() => {
    const raw = unwrapStr($selectedConnection?.capabilities);
    if (!raw) return [];
    try { return JSON.parse(raw); } catch { return []; }
  });

  let meta = $derived(getProviderMeta(providerId));
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="flex items-center gap-2 text-body-sm text-muted-foreground">
    <a href="/providers" class="hover:text-foreground transition-colors">Providers</a>
    <span>/</span>
    <a href="/providers/{providerId}" class="hover:text-foreground transition-colors">{meta?.displayName ?? providerId}</a>
    <span>/</span>
    <span class="text-foreground">{$selectedConnection?.name ?? 'Connection'}</span>
  </div>

  {#if $isLoading && !$selectedConnection}
    <div class="flex flex-col gap-6">
      <div class="flex items-center gap-4">
        <div class="size-12 bg-muted animate-pulse rounded-lg"></div>
        <div class="space-y-2">
          <div class="h-8 w-64 bg-muted animate-pulse rounded-md"></div>
          <div class="h-4 w-48 bg-muted/60 animate-pulse rounded-md"></div>
        </div>
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => loadConnection(connectionId)} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else if $selectedConnection}
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        <span class="size-3 rounded-full shrink-0" style="background-color: {getStatusDotColor($selectedConnection.status)}"></span>
        {#if editingName}
          <div class="flex items-center gap-2">
            <Input bind:value={editName} class="h-9 text-display-lg font-semibold w-64" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && handleSaveName()} />
            <Button onclick={handleSaveName} size="sm" class="h-8 text-body-sm rounded-sm">Save</Button>
            <Button onclick={() => editingName = false} variant="ghost" size="sm" class="h-8 text-body-sm">Cancel</Button>
          </div>
        {:else}
          <button class="text-display-lg cursor-pointer hover:opacity-80 transition-opacity text-left" onclick={startEditName} title="Click to rename">
            {$selectedConnection.name}.
          </button>
        {/if}
        <Badge variant={getStatusVariant($selectedConnection.status)} class="text-caption-mono rounded-sm">
          {getStatusLabel($selectedConnection.status)}
        </Badge>
        <Badge variant={$selectedConnection.is_active ? 'default' : 'outline'} class="text-caption-mono rounded-sm">
          {$selectedConnection.is_active ? 'Active' : 'Disabled'}
        </Badge>
      </div>
      <div class="flex items-center gap-2 text-caption-mono text-muted-foreground">
        <span>Auth: {$selectedConnection.auth_type}</span>
        <span>·</span>
        <span>Provider: {$selectedConnection.provider_type_id}</span>
        <span>·</span>
        <span>ID: {$selectedConnection.id}</span>
      </div>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <Card class="shadow-card">
        <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Details</CardTitle></CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Provider</p>
            <p class="text-code font-mono">{$selectedConnection.provider_type_id}</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Auth Type</p>
            <p class="text-code font-mono">{$selectedConnection.auth_type}</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Created</p>
            <p class="text-body-sm font-mono text-muted-foreground">{formatTimestamp($selectedConnection.created_at)}</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Last Updated</p>
            <p class="text-body-sm font-mono text-muted-foreground">{formatTimestamp($selectedConnection.updated_at)}</p>
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-card">
        <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Status & Failures</CardTitle></CardHeader>
        <CardContent class="grid grid-cols-2 gap-4">
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Status</p>
            <div class="flex items-center gap-2">
              <span class="size-2 rounded-full" style="background-color: {getStatusDotColor($selectedConnection.status)}"></span>
              <p class="text-body-sm font-medium">{getStatusLabel($selectedConnection.status)}</p>
            </div>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Cooldown</p>
            <p class="text-code font-mono">{$selectedConnection.cooldown_until ? formatCooldown($selectedConnection.cooldown_until) : 'None'}</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Failures</p>
            <p class="text-code font-mono {$selectedConnection.failure_count > 0 ? 'text-destructive font-semibold' : 'text-muted-foreground'}">
              {$selectedConnection.failure_count}
            </p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Last Success</p>
            <p class="text-body-sm font-mono text-muted-foreground">{formatTimestamp($selectedConnection.last_success_at)}</p>
          </div>
          {#if unwrapStr($selectedConnection.last_error)}
            <div class="col-span-2 space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Last Error</p>
              <p class="text-body-sm font-mono text-destructive break-all bg-destructive/5 p-2 rounded-md">{unwrapStr($selectedConnection.last_error)}</p>
            </div>
          {/if}
        </CardContent>
      </Card>
    </div>

    {#if capabilities.length > 0}
      <Card class="shadow-card">
        <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Capabilities</CardTitle></CardHeader>
        <CardContent>
          <div class="flex flex-wrap gap-1.5">
            {#each capabilities as capability}
              <Badge variant="secondary" class="text-caption-mono rounded-sm py-0.5">{capability}</Badge>
            {/each}
          </div>
        </CardContent>
      </Card>
    {/if}

    <Card class="shadow-card">
      <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Proxy Routing</CardTitle></CardHeader>
      <CardContent class="space-y-4">
        <p class="text-body-sm text-muted-foreground">Assign a proxy pool or group to route traffic for this connection.</p>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div class="space-y-2">
            <label for="proxy-group-select" class="text-body-sm-strong">Proxy Group</label>
            <select
              id="proxy-group-select"
              class="w-full h-9 rounded-md border border-input bg-background px-3 text-body-sm cursor-pointer"
              bind:value={selectedGroupId}
            >
              <option value="">None</option>
              {#each groups as group}
                <option value={group.id}>{group.name} ({group.mode})</option>
              {/each}
            </select>
          </div>
          <div class="space-y-2">
            <label for="proxy-pool-select" class="text-body-sm-strong">Proxy Pool</label>
            <select
              id="proxy-pool-select"
              class="w-full h-9 rounded-md border border-input bg-background px-3 text-body-sm cursor-pointer"
              bind:value={selectedPoolId}
            >
              <option value="">None</option>
              {#each pools as pool}
                <option value={pool.id}>{pool.name} ({pool.type})</option>
              {/each}
            </select>
          </div>
        </div>
        <Button onclick={saveProxyAssignment} disabled={proxySaving} class="text-body-sm rounded-sm">
          {proxySaving ? 'Saving...' : 'Save proxy assignment'}
        </Button>
      </CardContent>
    </Card>

    <Card class="shadow-card">
      <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Actions</CardTitle></CardHeader>
      <CardContent>
        <div class="flex flex-wrap gap-2">
          <Button onclick={handleTest} disabled={!!actionLoading} variant="outline" class="text-body-sm rounded-sm">
            {actionLoading === 'test' ? 'Testing...' : 'Test connection'}
          </Button>
          <Button onclick={handleReset} disabled={!!actionLoading} variant="outline" class="text-body-sm rounded-sm">
            {actionLoading === 'reset' ? 'Resetting...' : 'Reset status'}
          </Button>
          <Button onclick={handleToggle} disabled={!!actionLoading} variant="outline" class="text-body-sm rounded-sm">
            {actionLoading === 'toggle' ? 'Updating...' : ($selectedConnection.is_active ? 'Disable' : 'Enable')}
          </Button>
          <Button onclick={handleDelete} disabled={!!actionLoading} variant="destructive" class="text-body-sm rounded-sm ml-auto">
            {actionLoading === 'delete' ? 'Deleting...' : 'Delete connection'}
          </Button>
        </div>
      </CardContent>
    </Card>
  {/if}
</div>
