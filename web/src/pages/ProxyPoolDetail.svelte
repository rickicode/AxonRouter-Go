<script lang="ts">
  import { onMount } from 'svelte';
  import { proxyPoolsApi } from '$lib/api';
  import type { ProxyPool } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge, type BadgeVariant } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { toast } from 'svelte-sonner';
import * as AlertDialog from '$lib/components/ui/alert-dialog';
  import { router } from '$lib/router';

  let { id = '' }: { id?: string } = $props();
  let pool = $state<ProxyPool | null>(null);
  let loading = $state(true);
  let error = $state('');
  let editing = $state(false);
  let actionLoading = $state('');
let showDeleteConfirm = $state(false);

  // Edit form
  let editName = $state('');
  let editUrl = $state('');
  let editNoProxy = $state('');

  onMount(() => {
    document.title = 'Proxy Pool — AxonRouter';
    loadPool();
  });

  async function loadPool() {
    loading = true;
    error = '';
    try {
      const res = await proxyPoolsApi.get(id);
      pool = res.data;
      editName = pool.name;
      editUrl = pool.proxyUrl;
      editNoProxy = pool.noProxy ?? '';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load';
    } finally {
      loading = false;
    }
  }

  async function handleSave() {
    if (!pool || !editName.trim() || !editUrl.trim()) return;
    actionLoading = 'save';
    try {
      await proxyPoolsApi.update(pool.id, {
        name: editName.trim(),
        proxyUrl: editUrl.trim(),
        noProxy: editNoProxy.trim(),
      });
      editing = false;
      await loadPool();
      toast.success('Pool updated');
    } catch (err) {
      toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      actionLoading = '';
    }
  }

  async function handleTest() {
    if (!pool) return;
    actionLoading = 'test';
    try {
      const res = await proxyPoolsApi.test(pool.id);
      if (res.ok) {
        toast.success(`Proxy OK (${res.elapsedMs}ms)`);
      } else {
        toast.error(`Proxy failed: ${res.error || 'unknown'}`);
      }
      await loadPool();
    } catch (err) {
      toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      actionLoading = '';
    }
  }

  async function handleToggle() {
    if (!pool) return;
    actionLoading = 'toggle';
    try {
      await proxyPoolsApi.update(pool.id, { isActive: !pool.isActive });
      toast.success(pool.isActive ? 'Pool deactivated' : 'Pool activated');
      await loadPool();
    } catch (err) {
      toast.error('Toggle failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      actionLoading = '';
    }
  }

  function handleDelete() {
 showDeleteConfirm = true;
}

async function confirmDelete() {
 if (!pool) return;
 actionLoading = 'delete';
 try {
 await proxyPoolsApi.delete(pool.id);
 showDeleteConfirm = false;
 toast.success('Pool deleted');
 router.navigate('/proxy-pools');
 }
 catch (err) {
 toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
 }
 finally {
 actionLoading = '';
 }
}

function formatTimestamp(ts: string | null): string {
    if (!ts) return 'Never';
    return new Date(ts).toLocaleString();
  }

  function statusColor(status: string): BadgeVariant {
    if (status === 'active') return 'default';
    if (status === 'error') return 'destructive';
    return 'secondary';
  }

  function typeLabel(type: string): string {
    if (type === 'http') return 'HTTP';
    if (type === 'vercel') return 'Vercel';
    if (type === 'deno') return 'Deno';
    if (type === 'cloudflare') return 'Cloudflare';
    return type;
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="flex items-center gap-2 text-body-sm text-muted-foreground">
    <a href="/proxy-pools" class="hover:text-foreground transition-colors">Proxy Pools</a>
    <span>/</span>
    <span class="text-foreground">{pool?.name ?? 'Pool'}</span>
  </div>

  {#if loading}
    <div class="flex flex-col gap-6">
      <div class="h-8 w-64 bg-muted animate-pulse rounded-md"></div>
      <div class="h-40 bg-muted animate-pulse rounded-md"></div>
    </div>
  {:else if error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{error}</p>
        <Button onclick={loadPool} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else if pool}
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        {#if editing}
          <div class="flex items-center gap-2">
            <Input bind:value={editName} class="h-9 text-display-lg font-semibold w-full sm:w-64" onkeydown={(e) => e.key === 'Enter' && handleSave()} />
            <Button onclick={handleSave} size="sm" class="h-8 text-body-sm rounded-sm">Save</Button>
            <Button onclick={() => (editing = false)} variant="ghost" size="sm" class="h-8 text-body-sm">Cancel</Button>
          </div>
        {:else}
          <button class="text-display-lg cursor-pointer hover:opacity-80 transition-opacity text-left" onclick={() => (editing = true)} title="Click to edit">
            {pool.name}.
          </button>
        {/if}
        <Badge variant={pool.isActive ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
          {pool.isActive ? 'Active' : 'Disabled'}
        </Badge>
        <Badge variant={statusColor(pool.testStatus)} class="text-caption-mono rounded-sm">
          {pool.testStatus}
        </Badge>
      </div>
      <p class="text-caption-mono text-muted-foreground">ID: {pool.id}</p>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <Card class="shadow-card">
        <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Details</CardTitle></CardHeader>
        <CardContent class="space-y-4">
          {#if editing}
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Proxy URL</Label>
              <Input bind:value={editUrl} class="h-9 text-body-sm font-mono" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">No Proxy</Label>
              <Input bind:value={editNoProxy} placeholder="localhost,127.0.0.1" class="h-9 text-body-sm font-mono" />
            </div>
          {:else}
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Type</p>
              <p class="text-code font-mono">{typeLabel(pool.type)}</p>
            </div>
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Proxy URL</p>
              <p class="text-code font-mono break-all">{pool.proxyUrl}</p>
            </div>
            {#if pool.noProxy}
              <div class="space-y-1">
                <p class="text-caption-mono text-muted-foreground uppercase font-semibold">No Proxy</p>
                <p class="text-code font-mono">{pool.noProxy}</p>
              </div>
            {/if}
            {#if pool.relayAuth}
              <div class="space-y-1">
                <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Relay Auth</p>
                <p class="text-code font-mono break-all">{pool.relayAuth}</p>
              </div>
            {/if}
          {/if}
        </CardContent>
      </Card>

      <Card class="shadow-card">
        <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Status</CardTitle></CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Test Status</p>
            <div class="flex items-center gap-2">
              <Badge variant={statusColor(pool.testStatus)} class="text-caption-mono rounded-sm">{pool.testStatus}</Badge>
            </div>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Latency</p>
            <p class="text-code font-mono">{pool.responseTimeMs != null ? pool.responseTimeMs + 'ms' : '—'}</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Last Tested</p>
            <p class="text-body-sm font-mono text-muted-foreground">{formatTimestamp(pool.lastTestedAt)}</p>
          </div>
          {#if pool.lastError}
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Last Error</p>
              <p class="text-body-sm font-mono text-destructive break-all bg-destructive/5 p-2 rounded-md">{pool.lastError}</p>
            </div>
          {/if}
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Created</p>
            <p class="text-body-sm font-mono text-muted-foreground">{new Date(pool.createdAt * 1000).toLocaleString()}</p>
          </div>
        </CardContent>
      </Card>
    </div>

    <Card class="shadow-card">
      <CardHeader class="pb-3"><CardTitle class="text-body-md-strong">Actions</CardTitle></CardHeader>
      <CardContent>
        <div class="flex flex-wrap gap-2">
          <Button onclick={handleTest} disabled={!!actionLoading} variant="outline" class="text-body-sm rounded-sm">
            {actionLoading === 'test' ? 'Testing...' : 'Test proxy'}
          </Button>
          <Button onclick={handleToggle} disabled={!!actionLoading} variant="outline" class="text-body-sm rounded-sm">
            {actionLoading === 'toggle' ? 'Updating...' : (pool.isActive ? 'Disable' : 'Enable')}
          </Button>
          <Button onclick={handleDelete} disabled={!!actionLoading} variant="destructive" class="text-body-sm rounded-sm ml-auto">
            {actionLoading === 'delete' ? 'Deleting...' : 'Delete pool'}
          </Button>
        </div>
      </CardContent>
    </Card>

  <AlertDialog.Root bind:open={showDeleteConfirm}>
    <AlertDialog.Content>
      <AlertDialog.Header>
        <AlertDialog.Title>Delete proxy pool?</AlertDialog.Title>
        <AlertDialog.Description>
          This will permanently delete <strong>{pool?.name ?? 'this pool'}</strong> and clean up all references. This action cannot be undone.
        </AlertDialog.Description>
      </AlertDialog.Header>
      <AlertDialog.Footer>
        <AlertDialog.Cancel onclick={() => (showDeleteConfirm = false)}>Cancel</AlertDialog.Cancel>
        <AlertDialog.Action variant="destructive" onclick={confirmDelete}>
          {actionLoading === 'delete' ? 'Deleting...' : 'Delete'}
        </AlertDialog.Action>
      </AlertDialog.Footer>
    </AlertDialog.Content>
  </AlertDialog.Root>

 {/if}
</div>
