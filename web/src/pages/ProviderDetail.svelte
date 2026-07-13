<script lang="ts">
  import { onMount } from 'svelte';
  import { loadProvider, selectedProvider, loadConnections, connections, connectionPagination, connectionFilter, loadProviderModels, providerModels, modelTestResults, testProviderModel, addProviderModel, deleteProviderModel, isLoading, error } from '$lib/stores';
  import { unwrapInt, getTokenExpiry } from '$lib/utils';
  import { connectionsApi, providersApi } from '$lib/api';
import type { RoutingMode } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import * as Select from '$lib/components/ui/select';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';
  import { getProviderMeta, getStatusDotColor, getStatusVariant, getStatusLabel } from '$lib/provider-catalog';
  import { toast } from 'svelte-sonner';
import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
import Settings2Icon from '@lucide/svelte/icons/settings-2';
  import AddConnectionModal from '$lib/components/AddConnectionModal.svelte';
import ProviderRoutingModal from '$lib/components/ProviderRoutingModal.svelte';
import Pagination from '$lib/components/Pagination.svelte';
  import * as AlertDialog from '$lib/components/ui/alert-dialog';
import StatusBadge from '$lib/components/StatusBadge.svelte';

  let showAddModal = $state(false);
let showRoutingModal = $state(false);
let routingMode = $state<RoutingMode>('round_robin');

const routingModeLabels: Record<RoutingMode, string> = {
	round_robin: 'Round robin',
	random: 'Random',
	first_eligible: 'First eligible',
};
let newModel = $state('');

  let { id = '' }: { id?: string } = $props();
  let providerId = $derived(id);
  let meta = $derived(getProviderMeta(providerId));
  let currentPage = $state(1);
  let perPage = $state(50);
  let testingAll = $state(false);
  let actionLoading = $state('');
  let deleteTarget = $state<{ id: string; name: string } | null>(null);
  let deleteDialogOpen = $state(false);

  const statusOptions = [
    { value: '', label: 'All statuses' },
    { value: 'ready', label: 'Ready' },
    { value: 'rate_limited', label: 'Rate Limited' },
    { value: 'quota_exhausted', label: 'Quota Exhausted' },
    { value: 'auth_failed', label: 'Auth Failed' },
    { value: 'disabled', label: 'Disabled' },
  ];

  onMount(() => {
    document.title = `${meta?.displayName ?? 'Provider'} — AxonRouter`;
    loadProvider(providerId);
    loadConnections(providerId, currentPage, perPage);
    loadProviderModels(providerId);
    providersApi.getSettings(providerId).then((s) => {
        routingMode = s.routing_mode;
    }).catch(() => {
        // keep default
    });
});

  function formatCooldown(raw: unknown): string {
    const cooldownUntil = unwrapInt(raw);
    if (!cooldownUntil) return '—';
    const now = Math.floor(Date.now() / 1000);
    if (cooldownUntil <= now) return 'Expired';
    const seconds = cooldownUntil - now;
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
    return `${Math.floor(seconds / 3600)}h`;
  }

  function formatTimestamp(raw: unknown) {
    const ts = unwrapInt(raw);
    if (!ts) return '—';
    return new Date(ts * 1000).toLocaleDateString();
  }

  function isDefaultDirect(conn: any): boolean {
    if (!conn.provider_specific_data) return false;
    try {
      const psd = typeof conn.provider_specific_data === 'string' ? JSON.parse(conn.provider_specific_data) : conn.provider_specific_data;
      return psd?.direct === true || psd?.direct === 'true';
    } catch { return false; }
  }

  function getAccountLabel(conn: any): string | null {
    if (!conn.provider_specific_data) return null;
    try {
      const psd = typeof conn.provider_specific_data === 'string' ? JSON.parse(conn.provider_specific_data) : conn.provider_specific_data;
      return psd?.accountLabel || null;
    } catch { return null; }
  }

  function handlePageChange(page: number) {
    currentPage = page;
    loadConnections(providerId, currentPage, perPage);
  }

function handlePerPageChange(p: number) {
  perPage = p;
  currentPage = 1;
  loadConnections(providerId, currentPage, perPage);
}

  async function handleTestAll() {
    testingAll = true;
    try {
      const res = (await providersApi.test(providerId)) as any;
      await loadProvider(providerId);
      await loadConnections(providerId, currentPage, perPage);
      const results = res?.results ?? [];
      const ok = results.filter((r: any) => r.status === 'ok').length;
      const failed = results.filter((r: any) => r.status === 'failed').length;
      const skipped = results.filter((r: any) => r.status === 'skipped').length;
      if (failed > 0) {
        toast.error(`Test all: ${ok} passed, ${failed} failed${skipped ? `, ${skipped} skipped` : ''}`);
      } else {
        toast.success(`Test all: ${ok} passed${skipped ? `, ${skipped} skipped` : ''}`);
      }
    } catch (err) {
      toast.error('Test all failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally { testingAll = false; }
  }

  async function handleTestConnection(connId: string) {
    actionLoading = connId;
    try {
      const res = (await connectionsApi.test(connId)) as any;
      if (res?.success || res?.status === 'ok') {
        toast.success(`Connection OK (${res.latency_ms ?? 0}ms)`);
      } else {
        toast.error(`Test failed: ${res?.error ?? res?.message ?? 'Unknown error'}`);
      }
      await loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally { actionLoading = ''; }
  }

  async function handleResetConnection(connId: string) {
    actionLoading = connId;
    try {
      await connectionsApi.reset(connId);
      toast.success('Connection reset to ready');
      await loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      toast.error('Reset failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally { actionLoading = ''; }
  }
  async function handleRefreshToken(connId: string) {
    actionLoading = connId;
    try {
      const res = await connectionsApi.refreshToken(connId);
      toast.success(`Token refreshed, expires ${new Date(res.expires_at * 1000).toLocaleTimeString()}`);
      await loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      toast.error('Refresh failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally { actionLoading = ''; }
  }
  function confirmDeleteConnection(connId: string, name: string) {
    deleteTarget = { id: connId, name };
    deleteDialogOpen = true;
  }

  async function executeDeleteConnection() {
    if (!deleteTarget) return;
    const { id: connId, name } = deleteTarget;
    deleteTarget = null;
    deleteDialogOpen = false;
    actionLoading = connId;
    try {
      await connectionsApi.delete(connId);
      toast.success(`Deleted "${name}"`);
      await loadConnections(providerId, currentPage, perPage);
    } catch (err) {
      toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally { actionLoading = ''; }
  }

</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <a href="/providers" class="inline-flex items-center gap-1.5 text-body-sm text-muted-foreground hover:text-foreground transition-colors w-fit">
    <ArrowLeftIcon class="size-3.5" />
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
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => { loadProvider(providerId); loadConnections(providerId, currentPage, perPage); loadProviderModels(providerId); }} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else if $selectedProvider}
    <!-- Provider Header -->
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
          Prefix: {meta?.prefix ?? ($selectedProvider.id + '/')} · Format: {$selectedProvider.format} · ID: {$selectedProvider.id}
        </p>
      </div>
    </div>

    <!-- Connections Section -->
    <div class="space-y-4">
      <div class="flex items-center justify-between gap-3 flex-wrap">
        <div class="flex items-center gap-3">
          <h2 class="text-display-sm">Connections.</h2>
          <span class="text-caption-mono text-muted-foreground">{$connectionPagination.total} total</span>
        </div>
        <div class="flex items-center gap-2">
					<Badge variant="outline" class="text-caption-mono rounded-sm">{routingModeLabels[routingMode] ?? routingMode}</Badge>
          						<Button onclick={() => showRoutingModal = true} variant="outline" size="sm" class="text-body-sm rounded-sm gap-1.5">
							<Settings2Icon class="size-3.5" />
							Settings
						</Button>
					<Button onclick={handleTestAll} disabled={testingAll} variant="outline" size="sm" class="text-body-sm rounded-sm">
            {testingAll ? 'Testing...' : 'Test all'}
          </Button>
          <Button onclick={() => showAddModal = true} size="sm" class="text-body-sm rounded-sm">
            Add connection
          </Button>
        </div>
      </div>

      <div class="flex flex-wrap gap-3">
        <Select.Root
          type="single"
          value={$connectionFilter.status}
          onValueChange={(value: string) => { $connectionFilter.status = value || ''; currentPage = 1; loadConnections(providerId, currentPage, perPage); }}
        >
          <Select.Trigger class="w-full sm:w-[180px] h-9 text-body-sm rounded-sm">
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

      <Card class="shadow-card overflow-hidden">
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
                  <tr><td colspan="7" class="p-0">
                    <div class="flex flex-col items-center justify-center py-12 gap-3">
                      <div class="size-12 rounded-full bg-muted/50 flex items-center justify-center">
                        <svg class="size-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          {#if meta?.authType === 'oauth'}
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                          {:else}
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
                          {/if}
                        </svg>
                      </div>
                      <div class="text-center">
                        <p class="text-sm font-medium text-muted-foreground">No connections yet</p>
                        <p class="text-xs text-muted-foreground/70 mt-0.5">Add your first connection to get started</p>
                      </div>
                      <Button onclick={() => showAddModal = true} size="sm" class="text-body-sm rounded-sm mt-1">
                        Add connection
                      </Button>
                    </div>
                  </td></tr>
                {:else}
                  {#each $connections as row}
                    {@const isDefault = isDefaultDirect(row)}
                    {@const isOAuth = row.auth_type === 'oauth'}
                    {@const expiry = isOAuth ? getTokenExpiry(row.oauth_expires_at) : null}
                    <tr class="transition-colors hover:bg-accent/20 group">
                      <td class="py-3 px-4">
                        <a href="/providers/{providerId}/{row.id}" class="text-body-sm-strong hover:underline flex items-center gap-2">
                          <span class="size-2 rounded-full shrink-0" style="background-color: {getStatusDotColor(row.status)}"></span>
                          {row.name}
                        </a>
                        {#if isDefault}
                          <StatusBadge status="default" label="Default" class="ml-1" />
                        {/if}
                        {#if !isDefault && getAccountLabel(row)}
                          <StatusBadge status="smart" label={getAccountLabel(row)} class="ml-1" />
                        {/if}
                        {#if expiry}
						<StatusBadge status={expiry.status === 'expired' ? 'error' : expiry.status === 'expiring' ? 'testing' : 'active'} label={expiry.text} class="ml-1" />
					{/if}

                      </td>
                      <td class="py-3 px-4">
                        <Badge variant={getStatusVariant(row.status)} class="text-caption-mono rounded-sm py-0.5">
                          {getStatusLabel(row.status)}
                        </Badge>
                      </td>
                      <td class="py-3 px-4 text-code font-mono text-muted-foreground">{row.auth_type}</td>
                      <td class="py-3 px-4">
                        <span class="text-code font-mono {row.failure_count > 0 ? 'text-destructive font-semibold' : 'text-muted-foreground'}">{row.failure_count}</span>
                      </td>
                      <td class="py-3 px-4 text-code font-mono text-muted-foreground">
                        {formatCooldown(row.cooldown_until)}
                      </td>
                      <td class="py-3 px-4 text-body-sm text-muted-foreground">{formatTimestamp(row.last_success_at)}</td>
                      <td class="py-3 px-4">
                        <div class="flex gap-1">
                          <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm" disabled={actionLoading === row.id} onclick={() => handleTestConnection(row.id)}>
                            {actionLoading === row.id ? '...' : 'Test'}
                          </Button>
                          {#if isOAuth}
                            <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm text-amber-400 hover:text-amber-300" disabled={actionLoading === row.id} onclick={() => handleRefreshToken(row.id)}>
                              Refresh
                            </Button>
                          {/if}
                          <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm" disabled={actionLoading === row.id} onclick={() => handleResetConnection(row.id)}>
                            Reset
                          </Button>
                          {#if !isDefault}
                            <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm text-destructive hover:text-destructive" disabled={actionLoading === row.id} onclick={() => confirmDeleteConnection(row.id, row.name)}>
                              Del
                            </Button>
                          {/if}
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

<Pagination
  page={currentPage}
  totalPages={$connectionPagination.total_pages}
  total={$connectionPagination.total}
  perPage={perPage}
  perPageOptions={[25, 50, 100]}
  onPerPageChange={handlePerPageChange}
  onChange={handlePageChange}
/>

</div>


<!-- Models Section (below connections) -->
<div class="space-y-4">
<div class="flex items-center gap-2">
 <Input bind:value={newModel} placeholder="Add model (e.g. my-model)" class="text-body-sm rounded-sm" />
 <Button onclick={async () => { if (!newModel.trim()) return; await addProviderModel(providerId, newModel.trim()); newModel = ''; }} variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer" disabled={!newModel.trim()}>Add model</Button>
</div>
          {#each $providerModels as model}
            {@const result = $modelTestResults[model]}
            <button
              class="group inline-flex items-center gap-2 px-3 py-1.5 bg-card border border-border rounded-md hover:border-primary/40 transition-colors cursor-pointer disabled:opacity-50"
              disabled={result?.status === 'testing'}
              onclick={() => testProviderModel(providerId, model)}
              title={model}
            >
              <span class="text-[12px] font-mono truncate max-w-[180px]">{model}</span>
              {#if result}
                <span class="size-1.5 rounded-full shrink-0 {result.status === 'ok' ? 'bg-emerald-500' : result.status === 'testing' ? 'bg-yellow-500 animate-pulse' : 'bg-destructive'}"></span>
                {#if result.latency_ms}
                  <span class="text-[10px] font-mono text-muted-foreground">{result.latency_ms}ms</span>
                {/if}
              {/if}
 <span
  class="ml-1 size-4 inline-flex items-center justify-center rounded-sm text-muted-foreground hover:text-destructive hover:bg-destructive/10 cursor-pointer"
  title="Remove model"
   role="button"
   tabindex="0"
   aria-label="Remove model"
  onclick={(e) => { e.stopPropagation(); deleteProviderModel(providerId, model); }}
					onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); deleteProviderModel(providerId, model); } }}
 >×</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>


<AddConnectionModal bind:open={showAddModal} {providerId} {meta} onCreated={() => { loadConnections(providerId, currentPage, perPage); loadProvider(providerId); loadProviderModels(providerId); }} />
<ProviderRoutingModal bind:open={showRoutingModal} {providerId} currentMode={routingMode} onSaved={(mode) => (routingMode = mode)} />

<AlertDialog.Root bind:open={deleteDialogOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>Delete connection</AlertDialog.Title>
      <AlertDialog.Description>
        Delete "{deleteTarget?.name ?? ''}"? This cannot be undone.
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Action onclick={executeDeleteConnection}>Delete</AlertDialog.Action>
      <AlertDialog.Cancel onclick={() => { deleteTarget = null; deleteDialogOpen = false; }}>Cancel</AlertDialog.Cancel>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
