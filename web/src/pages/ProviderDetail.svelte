<script lang="ts">
  import { onMount } from 'svelte';
  import {
    loadProvider, selectedProvider,
    loadConnections, connections, connectionPagination, connectionFilter,
    loadProviderModels, providerModels, testProviderModel, modelTestResults,
    isLoading, error
  } from '$lib/stores';
  import { connectionsApi, providersApi } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import * as Select from '$lib/components/ui/select';
  import { getProviderMeta, getStatusDotColor, getStatusVariant, getStatusLabel } from '$lib/provider-catalog';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';

  let { id = '' }: { id?: string } = $props();
  let providerId = $derived(id);
  let currentPage = $state(1);
  let perPage = $state(50);
  let testingAll = $state(false);
  let bulkKeys = $state('');
  let showBulkAdd = $state(false);
  let bulkResult = $state<{ success: number; failed: number; errors: string[] } | null>(null);
  let bulkLoading = $state(false);

  let meta = $derived(getProviderMeta(providerId));

  onMount(() => {
    document.title = 'Provider - AxonRouter';
    loadProvider(providerId);
    loadConnections(providerId, currentPage, perPage);
    loadProviderModels(providerId);
  });

  function handlePageChange(p: number) {
    currentPage = p;
    loadConnections(providerId, currentPage, perPage);
  }

  function formatCooldown(cooldownUntil: number | null): string {
    if (!cooldownUntil) return '—';
    const now = Math.floor(Date.now() / 1000);
    if (cooldownUntil <= now) return 'Expired';
    const seconds = cooldownUntil - now;
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  }

  function formatTimestamp(ts: number | null): string {
    if (!ts) return 'Never';
    return new Date(ts * 1000).toLocaleString();
  }

  async function handleTestConnection(connId: string) {
    try {
      await connectionsApi.test(connId);
      loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      alert('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleResetConnection(connId: string) {
    try {
      await connectionsApi.reset(connId);
      loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      alert('Reset failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleDeleteConnection(connId: string, name: string) {
    if (!confirm(`Delete connection "${name}"? This cannot be undone.`)) return;
    try {
      await connectionsApi.delete(connId);
      loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      alert('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleBulkAdd() {
    const keys = bulkKeys.split('\n').map(k => k.trim()).filter(Boolean);
    if (keys.length === 0) return;
    bulkLoading = true;
    bulkResult = null;
    try {
      bulkResult = await connectionsApi.bulkCreate(providerId, { api_keys: keys });
      loadConnections(providerId, currentPage, perPage);
      if (bulkResult.failed === 0) {
        bulkKeys = '';
        showBulkAdd = false;
      }
    } catch (err) {
      alert('Bulk add failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      bulkLoading = false;
    }
  }

  async function handleTestAll() {
    testingAll = true;
    try {
      await providersApi.test(providerId);
      loadConnections(providerId, currentPage, perPage);
    } catch {
      // ignore
    } finally {
      testingAll = false;
    }
  }

  async function handleOAuth(connId: string) {
    try {
      const result = await connectionsApi.initiateOAuth(connId);
      window.open(result.auth_url, '_blank');
    } catch (err) {
      alert('OAuth failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  const statusOptions = [
    { value: '', label: 'All statuses' },
    { value: 'ready', label: 'Ready' },
    { value: 'rate_limited', label: 'Rate limited' },
    { value: 'quota_exhausted', label: 'Quota exhausted' },
    { value: 'balance_empty', label: 'Balance empty' },
    { value: 'auth_failed', label: 'Auth failed' },
    { value: 'suspended', label: 'Suspended' },
    { value: 'disabled', label: 'Disabled' },
  ];
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <a href="/providers" class="inline-flex items-center gap-1.5 text-body-sm text-muted-foreground hover:text-foreground transition-colors w-fit">
    <svg class="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
    Back to providers
  </a>

  {#if $isLoading && !$selectedProvider}
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
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => { loadProvider(providerId); loadConnections(providerId, currentPage, perPage); loadProviderModels(providerId); }} variant="outline">Try again</Button>
      </CardContent>
    </Card>
  {:else if $selectedProvider}
    <div class="flex items-start gap-4">
      <div
        class="size-12 rounded-lg flex items-center justify-center shrink-0 overflow-hidden"
        style="background-color: {(meta?.color ?? '#888')}15"
      >
        <ProviderIcon {meta} size={48} />
      </div>
      <div class="space-y-1 min-w-0">
        <div class="flex items-center gap-3 flex-wrap">
          <h1 class="text-display-lg">{$selectedProvider.display_name}.</h1>
          {#if $selectedProvider.is_custom}
            <Badge variant="secondary" class="text-caption-mono rounded-sm">Custom</Badge>
          {/if}
          {#if meta}
            <Badge variant="outline" class="text-caption-mono rounded-sm">{meta.category}</Badge>
          {/if}
        </div>
        <p class="text-caption-mono text-muted-foreground">
          Prefix: {meta?.prefix ?? '—'} · Format: {$selectedProvider.format} · ID: {$selectedProvider.id}
        </p>
      </div>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3"><CardTitle class="text-body-md font-semibold">Status Summary</CardTitle></CardHeader>
        <CardContent>
          {#if $selectedProvider.status_counts}
            <div class="grid grid-cols-2 gap-3">
              {#each Object.entries($selectedProvider.status_counts) as [status, count]}
                {#if count > 0 || status === 'ready'}
                  <div class="flex items-center gap-2">
                    <span class="size-2 rounded-full shrink-0" style="background-color: {getStatusDotColor(status)}"></span>
                    <span class="text-body-sm text-muted-foreground">{getStatusLabel(status)}</span>
                    <span class="text-body-sm font-semibold font-mono ml-auto">{count}</span>
                  </div>
                {/if}
              {/each}
            </div>
          {:else}
            <p class="text-body-sm text-muted-foreground">No connection data available.</p>
          {/if}
        </CardContent>
      </Card>

      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3"><CardTitle class="text-body-md font-semibold">Actions</CardTitle></CardHeader>
        <CardContent class="flex flex-wrap gap-2">
          <Button onclick={handleTestAll} disabled={testingAll} variant="outline" size="sm" class="text-body-sm">
            {testingAll ? 'Testing...' : 'Test all'}
          </Button>
          <Button onclick={() => showBulkAdd = !showBulkAdd} variant="outline" size="sm" class="text-body-sm">
            Bulk add
          </Button>
          <a href="/providers/add" class="inline-flex items-center justify-center h-8 px-3 text-body-sm bg-foreground text-background rounded-md hover:bg-foreground/90 transition-colors">
            Add connection
          </a>
        </CardContent>
      </Card>
    </div>

    {#if showBulkAdd}
      <Card class="shadow-vercel-2 border border-primary/20">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Bulk Add Connections</CardTitle>
          <p class="text-body-sm text-muted-foreground">Paste one API key per line. Each key becomes a connection.</p>
        </CardHeader>
        <CardContent class="space-y-3">
          <textarea
            class="w-full h-32 bg-input border border-border rounded-md p-3 font-mono text-body-sm text-foreground placeholder:text-muted-foreground resize-y focus:outline-none focus:ring-1 focus:ring-ring"
            placeholder="sk-...&#10;sk-...&#10;sk-..."
            bind:value={bulkKeys}
          ></textarea>
          <div class="flex items-center gap-3">
            <Button onclick={handleBulkAdd} disabled={bulkLoading || !bulkKeys.trim()} size="sm" class="text-body-sm">
              {bulkLoading ? 'Adding...' : `Add ${bulkKeys.split('\n').filter(k => k.trim()).length} keys`}
            </Button>
            <Button onclick={() => { showBulkAdd = false; bulkKeys = ''; bulkResult = null; }} variant="ghost" size="sm" class="text-body-sm">
              Cancel
            </Button>
          </div>
          {#if bulkResult}
            <div class="flex gap-4 text-body-sm p-3 rounded-md {bulkResult.failed > 0 ? 'bg-destructive/10 text-destructive' : 'bg-green-500/10 text-green-400'}">
              <span>✓ {bulkResult.success} added</span>
              {#if bulkResult.failed > 0}
                <span>✗ {bulkResult.failed} failed</span>
              {/if}
            </div>
          {/if}
        </CardContent>
      </Card>
    {/if}

    <!-- Models Section -->
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-display-sm">Models.</h2>
        <span class="text-caption-mono text-muted-foreground">{$providerModels.length} available</span>
      </div>
      <Card class="shadow-vercel-2 border overflow-hidden">
        <CardContent class="p-0">
          <div class="overflow-x-auto">
            <table class="w-full text-left border-collapse">
              <thead>
                <tr class="border-b border-border bg-muted/30">
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Model</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-32">Status</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-24">Latency</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-24"></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#if $providerModels.length === 0}
                  <tr><td colspan="4" class="p-8 text-center text-body-sm text-muted-foreground">No models discovered yet.</td></tr>
                {:else}
                  {#each $providerModels as model}
                    {@const result = $modelTestResults[model]}
                    <tr class="transition-colors hover:bg-accent/20">
                      <td class="py-3 px-4 font-mono text-body-sm">{model}</td>
                      <td class="py-3 px-4">
                        {#if result}
                          <Badge variant={result.status === 'ok' ? 'default' : result.status === 'testing' ? 'secondary' : 'destructive'} class="text-caption-mono rounded-sm">
                            {result.status}
                          </Badge>
                        {:else}
                          <span class="text-body-sm text-muted-foreground">—</span>
                        {/if}
                      </td>
                      <td class="py-3 px-4 text-body-sm text-muted-foreground font-mono">
                        {result?.latency_ms ? `${result.latency_ms}ms` : '—'}
                      </td>
                      <td class="py-3 px-4">
                        <Button
                          variant="ghost"
                          size="sm"
                          class="text-body-sm h-8 px-2.5"
                          disabled={result?.status === 'testing'}
                          onclick={() => testProviderModel(providerId, model)}
                        >
                          {result?.status === 'testing' ? 'Testing...' : 'Test'}
                        </Button>
                      </td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Connections Section -->
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-display-sm">Connections.</h2>
        <span class="text-caption-mono text-muted-foreground">{$connectionPagination.total} total</span>
      </div>

      <div class="flex flex-wrap gap-3">
        <Select.Root
          value={$connectionFilter.status}
          onValueChange={(value: string) => { $connectionFilter.status = value || ''; currentPage = 1; loadConnections(providerId, currentPage, perPage); }}
        >
          <Select.Trigger class="w-[180px] h-9 text-body-sm">
            {statusOptions.find(o => o.value === $connectionFilter.status)?.label || 'All statuses'}
          </Select.Trigger>
          <Select.Content>
            {#each statusOptions as option}
              <Select.Item value={option.value} class="text-body-sm">{option.label}</Select.Item>
            {/each}
          </Select.Content>
        </Select.Root>
        <Input type="text" class="w-64 h-9 text-body-sm" placeholder="Search connections..." bind:value={$connectionFilter.search} oninput={() => { currentPage = 1; loadConnections(providerId, currentPage, perPage); }} />
      </div>

      <Card class="shadow-vercel-2 border overflow-hidden">
        <CardContent class="p-0">
          <div class="overflow-x-auto">
            <table class="w-full text-left border-collapse">
              <thead>
                <tr class="border-b border-border bg-muted/30">
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Name</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Status</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Auth</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Failures</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Cooldown</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Last success</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-44"></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#if $connections.length === 0}
                  <tr><td colspan="7" class="p-8 text-center text-body-sm text-muted-foreground">No connections found.</td></tr>
                {:else}
                  {#each $connections as row}
                    <tr class="transition-colors hover:bg-accent/20 group">
                      <td class="py-3 px-4">
                        <a href="/providers/{providerId}/{row.id}" class="font-medium text-body-sm hover:underline flex items-center gap-2">
                          <span class="size-2 rounded-full shrink-0" style="background-color: {getStatusDotColor(row.status)}"></span>
                          {row.name}
                        </a>
                      </td>
                      <td class="py-3 px-4">
                        <Badge variant={getStatusVariant(row.status)} class="text-caption-mono rounded-sm py-0.5">
                          {getStatusLabel(row.status)}
                        </Badge>
                      </td>
                      <td class="py-3 px-4 font-mono text-xs text-muted-foreground">{row.auth_type}</td>
                      <td class="py-3 px-4 text-body-sm">
                        <span class={row.failure_count > 0 ? 'text-destructive font-medium font-mono' : 'text-muted-foreground font-mono'}>{row.failure_count}</span>
                      </td>
                      <td class="py-3 px-4 text-body-sm font-mono text-muted-foreground">
                        {formatCooldown(row.cooldown_until)}
                      </td>
                      <td class="py-3 px-4 text-body-sm text-muted-foreground">{formatTimestamp(row.last_success_at)}</td>
                      <td class="py-3 px-4">
                        <div class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                          <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2" onclick={() => handleTestConnection(row.id)}>Test</Button>
                          {#if row.auth_type === 'oauth'}
                            <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2" onclick={() => handleOAuth(row.id)}>OAuth</Button>
                          {/if}
                          <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2" onclick={() => handleResetConnection(row.id)}>Reset</Button>
                          <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 text-destructive hover:text-destructive" onclick={() => handleDeleteConnection(row.id, row.name)}>Del</Button>
                        </div>
                      </td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {#if $connectionPagination.total_pages > 1}
        <div class="flex items-center justify-between">
          <p class="text-body-sm text-muted-foreground">
            Showing {((currentPage - 1) * perPage) + 1}–{Math.min(currentPage * perPage, $connectionPagination.total)} of {$connectionPagination.total}
          </p>
          <div class="flex gap-1">
            <Button variant="outline" size="sm" disabled={currentPage === 1} onclick={() => handlePageChange(currentPage - 1)} class="text-body-sm h-8">Prev</Button>
            {#each Array.from({ length: Math.min(5, $connectionPagination.total_pages) }, (_, i) => i + Math.max(1, currentPage - 2)) as p}
              {#if p <= $connectionPagination.total_pages}
                <Button variant={p === currentPage ? 'default' : 'outline'} size="sm" onclick={() => handlePageChange(p)} class="text-body-sm h-8 w-8 p-0">{p}</Button>
              {/if}
            {/each}
            <Button variant="outline" size="sm" disabled={currentPage === $connectionPagination.total_pages} onclick={() => handlePageChange(currentPage + 1)} class="text-body-sm h-8">Next</Button>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>
