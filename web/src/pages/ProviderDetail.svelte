<script lang="ts">
 import { onMount } from 'svelte';
 import { loadProvider, selectedProvider, loadConnections, connections, connectionPagination, connectionFilter, loadProviderModels, providerModels, modelTestResults, testProviderModel, addProviderModel, deleteProviderModel, isLoading, error } from '$lib/stores';
 import { unwrapInt, getTokenExpiry } from '$lib/utils';
import { copyToClipboard } from '$lib/copy';
 import { connectionsApi, providersApi, proxyPoolsApi } from '$lib/api';
import type { RoutingMode, ProviderModelEntry, ProxyPool, Connection } from '$lib/api';
 import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
 import { Button } from '$lib/components/ui/button';
 import { Badge } from '$lib/components/ui/badge';
 import { Input } from '$lib/components/ui/input';
 import * as Select from '$lib/components/ui/select';
 import ProviderIcon from '$lib/components/ProviderIcon.svelte';
 import { getProviderMeta, getCategoryById, getStatusDotColor, getStatusVariant, getStatusLabel } from '$lib/provider-catalog';
 import { toast } from 'svelte-sonner';
import Icon from '$lib/components/Icon.svelte';
 import AddConnectionModal from '$lib/components/AddConnectionModal.svelte';
import ProviderRoutingModal from '$lib/components/ProviderRoutingModal.svelte';
import ProviderEditModal from '$lib/components/ProviderEditModal.svelte';
import Pagination from '$lib/components/Pagination.svelte';
 import * as AlertDialog from '$lib/components/ui/alert-dialog';
import StatusBadge from '$lib/components/StatusBadge.svelte';

 let showAddModal = $state(false);
let showRoutingModal = $state(false);
let showEditModal = $state(false);
let routingMode = $state<RoutingMode>('round_robin');

const routingModeLabels: Record<RoutingMode, string> = {
 round_robin: 'Round robin',
 random: 'Random',
 first_eligible: 'First eligible',
affinity: 'Session affinity',
};
let newModel = $state('');

 let { id = '' }: { id?: string } = $props();
 let providerId = $derived(id);
 let meta = $derived(getProviderMeta(providerId));
 let currentPage = $state(1);
 let perPage = $state(50);
 let testingAll = $state(false);
 let actionLoading = $state<{ connectionId: string; action: 'test' | 'reset' | 'refresh' | 'delete' } | null>(null);
  let deleteTarget = $state<{ id: string; name: string } | null>(null);
  let deleteDialogOpen = $state(false);

  // Bulk proxy assignment state
  let selectedConnectionIds = $state<Set<string>>(new Set());
  let proxyPools = $state<ProxyPool[]>([]);
  let selectedProxyPoolId = $state('');
  let bulkAssigning = $state(false);

  const needsProxyPool = $derived(providerId === 'oc' || providerId === 'mimocode');
  const selectedCount = $derived(selectedConnectionIds.size);
  const allVisibleSelected = $derived($connections.length > 0 && $connections.every((c) => selectedConnectionIds.has(c.id)));
  const canApplyProxy = $derived(selectedCount > 0);

 let providerCategoryId = $derived($selectedProvider?.category ?? meta?.category ?? 'compatible');
 let providerCategoryLabel = $derived(getCategoryById(providerCategoryId)?.label ?? providerCategoryId);
 let providerServiceKinds = $derived.by(() => {
   const kinds = $selectedProvider?.service_kinds ?? meta?.serviceKinds ?? ['llm'];
   return kinds.length === 1 && kinds[0] === 'llm' ? [] : kinds;
 });

const statusOptions = [
	{ value: '', label: 'All statuses' },
	{ value: 'ready', label: 'Ready' },
	{ value: 'rate_limited', label: 'Rate Limited' },
	{ value: 'quota_exhausted', label: 'Quota Exhausted' },
	{ value: 'disabled', label: 'Disabled' },
];

const MODEL_KIND_ORDER: [string, string][] = [
	['llm', 'Chat / Text'],
	['image', 'Image'],
	['embedding', 'Embedding'],
	['tts', 'Text-to-Speech'],
	['stt', 'Speech-to-Text'],
	['imageToText', 'Image-to-Text'],
	['other', 'Other'],
];

let groupedProviderModels = $derived.by(() => {
	const groups = new Map<string, ProviderModelEntry[]>();
	for (const [kind] of MODEL_KIND_ORDER) {
		groups.set(kind, []);
	}
	for (const entry of $providerModels) {
		const kinds = entry.service_kinds ?? [];
		let placed = false;
		for (const [kind] of MODEL_KIND_ORDER) {
			if (kind === 'other') continue;
			if (kinds.includes(kind)) {
				groups.get(kind)!.push(entry);
				placed = true;
			}
		}
		if (!placed) {
			groups.get('other')!.push(entry);
		}
	}
	for (const list of groups.values()) {
		list.sort((a, b) => a.id.localeCompare(b.id));
	}
	return groups;
});

 onMount(() => {
  document.title = `${meta?.displayName ?? 'Provider'} — AxonRouter`;
  loadProvider(providerId);
  refreshConnections();
  loadProviderModels(providerId);
  loadProxyPools();
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

async function copyModelName(id: string) {
	await copyToClipboard(id, 'Model name');
}

 function isDefaultDirect(conn: any): boolean {
 if (!conn.provider_specific_data) return false;
 try {
 const psd = typeof conn.provider_specific_data === 'string' ? JSON.parse(conn.provider_specific_data) : conn.provider_specific_data;
 return psd?.direct === true || psd?.direct === 'true';
 } catch { return false; }
 }

  function getAccountLabel(conn: any): string | undefined {
  if (!conn.provider_specific_data) return undefined;
  try {
  const psd = typeof conn.provider_specific_data === 'string' ? JSON.parse(conn.provider_specific_data) : conn.provider_specific_data;
  return psd?.accountLabel || undefined;
  } catch { return undefined; }
  }

  function getKiroAuthMethod(conn: any): string | undefined {
  if (providerId !== 'kiro' || !conn.provider_specific_data) return undefined;
  try {
  const psd = typeof conn.provider_specific_data === 'string' ? JSON.parse(conn.provider_specific_data) : conn.provider_specific_data;
  const authMethod = psd?.authMethod || '';
  const map: Record<string, string> = {
    'builder-id': 'AWS Builder ID',
    'idc': 'IAM Identity Center',
    'google': 'Google',
    'github': 'GitHub',
    'external_idp': 'External IdP',
    'api_key': 'API Key',
    'imported': 'Imported',
    'import': 'Imported',
  };
  return map[authMethod];
  } catch { return undefined; }
  }


 function handlePageChange(page: number) {
 currentPage = page;
 refreshConnections();
 }

function handlePerPageChange(p: number) {
 perPage = p;
 refreshConnections(true);
 }

function currentConnectionFilter(): { status: string; search: string } {
 let status = '';
 let search = '';
 connectionFilter.subscribe((f) => {
 status = f.status;
 search = f.search;
 })();
 return { status, search };
}

async function loadAllConnections() {
 isLoading.set(true);
 error.set(null);
 try {
 const filter = currentConnectionFilter();
 const all: Connection[] = [];
 let pageNum = 1;
 let totalPages = 1;
 do {
 const response = await connectionsApi.list(providerId, {
 page: pageNum,
 per_page: 200,
 status: filter.status || undefined,
 search: filter.search || undefined,
 });
 all.push(...(response.data || []));
 totalPages = response.pagination?.total_pages ?? 1;
 pageNum++;
 } while (pageNum <= totalPages);
 connections.set(all);
 connectionPagination.set({
 page: 1,
 per_page: all.length,
 total: all.length,
 total_pages: 1,
 });
 currentPage = 1;
 } catch (err) {
 error.set(err instanceof Error ? err.message : 'Failed to load all connections');
 toast.error('Failed to load all connections');
 } finally {
 isLoading.set(false);
 }
}

function refreshConnections(resetPage = false) {
 if (resetPage) currentPage = 1;
 if (perPage === 0) return loadAllConnections();
 return loadConnections(providerId, currentPage, perPage);
}

async function testConnectionAndRefresh(conn: Connection): Promise<boolean> {
 actionLoading = { connectionId: conn.id, action: 'test' };
 try {
 const res = (await connectionsApi.test(conn.id)) as any;
 const ok = res?.status === 'ok' || res?.success;
 if (!ok) {
 toast.error(`Test failed for ${conn.name ?? conn.id}: ${res?.error ?? res?.message ?? 'Unknown error'}`);
 }
 return ok;
 } catch (err) {
 toast.error(`Test failed for ${conn.name ?? conn.id}: ${err instanceof Error ? err.message : 'Unknown'}`);
 return false;
 } finally {
 // Refresh just this row so the status badge updates inline.
 try {
 const fresh = await connectionsApi.get(conn.id);
 connections.update((list) => list.map((c) => (c.id === fresh.id ? fresh : c)));
 } catch (_) {
 // Ignore refresh errors; the next full reload will catch up.
 }
 }
}

  async function handleTestAll() {
 if ($connections.length === 0) {
 toast.info('No connections to test');
 return;
 }
 testingAll = true;
 let passed = 0;
 let failed = 0;
 try {
 const conns = $connections;
 // For large provider lists, run tests with limited concurrency so we
 // don't hammer the backend while still finishing in a reasonable time.
 const parallel = conns.length > 100 ? 2 : 1;
 if (parallel === 1) {
 for (const conn of conns) {
 if (await testConnectionAndRefresh(conn)) passed++;
 else failed++;
 }
 } else {
 let index = 0;
 async function worker() {
 while (index < conns.length) {
 const conn = conns[index++];
 if (await testConnectionAndRefresh(conn)) passed++;
 else failed++;
 }
 }
 await Promise.all(Array.from({ length: parallel }, worker));
 }
 } finally {
 actionLoading = null;
 testingAll = false;
 }
 if (failed > 0) {
 toast.error(`Test all: ${passed} passed, ${failed} failed`);
 } else {
 toast.success(`Test all: ${passed} passed`);
 }
 }

async function handleTestConnection(connId: string) {
  actionLoading = { connectionId: connId, action: 'test' };
  try {
 const res = (await connectionsApi.test(connId)) as any;
 if (res?.success || res?.status === 'ok') {
 toast.success(`Connection OK (${res.latency_ms ?? 0}ms)`);
 } else {
 toast.error(`Test failed: ${res?.error ?? res?.message ?? 'Unknown error'}`);
 }
 await refreshConnections();
 } catch (err) {
 toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally { actionLoading = null; }
}

async function handleResetConnection(connId: string) {
  actionLoading = { connectionId: connId, action: 'reset' };
  try {
 await connectionsApi.reset(connId);
 toast.success('Connection reset to ready');
 await refreshConnections();
 } catch (err) {
 toast.error('Reset failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally { actionLoading = null; }
}

async function handleRefreshToken(connId: string) {
  actionLoading = { connectionId: connId, action: 'refresh' };
  try {
 const res = await connectionsApi.refreshToken(connId);
 toast.success(`Token refreshed, expires ${new Date(res.expires_at * 1000).toLocaleTimeString()}`);
 await refreshConnections();
 } catch (err) {
 toast.error('Refresh failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally { actionLoading = null; }
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
   actionLoading = { connectionId: connId, action: 'delete' };
   try {
     await connectionsApi.delete(connId);
  toast.success(`Deleted "${name}"`);
  await refreshConnections();
  } catch (err) {
  toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
   } finally { actionLoading = null; }
}

function toggleSelectConnection(id: string) {
  if (selectedConnectionIds.has(id)) {
    selectedConnectionIds.delete(id);
  } else {
    selectedConnectionIds.add(id);
  }
  selectedConnectionIds = new Set(selectedConnectionIds);
}

function toggleSelectAll() {
  if (allVisibleSelected) {
    for (const c of $connections) selectedConnectionIds.delete(c.id);
  } else {
    for (const c of $connections) selectedConnectionIds.add(c.id);
  }
  selectedConnectionIds = new Set(selectedConnectionIds);
}

function clearSelection() {
  selectedConnectionIds = new Set();
}

async function loadProxyPools() {
  if (!needsProxyPool) return;
  try {
    proxyPools = await proxyPoolsApi.listAll();
  } catch (err) {
    toast.error('Failed to load proxy pools: ' + (err instanceof Error ? err.message : 'Unknown'));
  }
}

async function handleBulkAssignProxy() {
  if (!needsProxyPool || selectedCount === 0) return;
  bulkAssigning = true;
  try {
    const poolId = selectedProxyPoolId || null;
    const res = await connectionsApi.bulkAssignProxy(providerId, {
      connection_ids: [...selectedConnectionIds],
      proxy_pool_id: poolId,
    });
    toast.success(`Proxy pool updated for ${res.updated} connection${res.updated === 1 ? '' : 's'}`);
    clearSelection();
    await refreshConnections();
  } catch (err) {
    toast.error('Bulk proxy assignment failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    bulkAssigning = false;
  }
}

 </script>

{#snippet connectionBadges(row: any)}
  {@const isDefault = isDefaultDirect(row)}
  {@const isOAuth = row.auth_type === 'oauth'}
  {@const expiry = isOAuth ? getTokenExpiry(row.oauth_expires_at) : null}
  <span class="inline-flex flex-wrap items-center gap-1">
    {#if isDefault}
      <StatusBadge status="default" label="Default" />
    {/if}
    {#if !isDefault && getAccountLabel(row)}
      <StatusBadge status="smart" label={getAccountLabel(row)} />
    {/if}
    {#if expiry}
      <StatusBadge status={expiry.status === 'expired' ? 'error' : expiry.status === 'expiring' ? 'testing' : 'active'} label={expiry.text} />
    {/if}
  </span>
{/snippet}

{#snippet connectionActions(row: any, btnClass = 'size-7', btnVariant: 'ghost' | 'outline' = 'ghost', iconSize = 'size-4')}
  {@const isDefault = isDefaultDirect(row)}
  {@const isOAuth = row.auth_type === 'oauth'}
  {@const busy = !!actionLoading && actionLoading.connectionId === row.id}
  {@const active = (a: 'test' | 'reset' | 'refresh' | 'delete') => busy && actionLoading?.action === a}
  <Button variant={btnVariant} size="icon" class={btnClass} href={`/providers/${providerId}/${row.id}`} title="Edit connection" aria-label="Edit connection" disabled={busy}>
    <Icon name="pencil" class={iconSize} />
  </Button>
  <Button variant={btnVariant} size="icon" class={btnClass} onclick={() => handleTestConnection(row.id)} title="Test connection" aria-label="Test connection" disabled={busy}>
    <Icon name={active('test') ? 'refreshCw' : 'play'} class={active('test') ? `${iconSize} animate-spin` : iconSize} />
  </Button>
  {#if isOAuth}
    <Button variant={btnVariant} size="icon" class={`${btnClass} text-amber-400 hover:text-amber-300`} onclick={() => handleRefreshToken(row.id)} title="Refresh token" aria-label="Refresh token" disabled={busy}>
      <Icon name="refreshCw" class={active('refresh') ? `${iconSize} animate-spin` : iconSize} />
    </Button>
  {/if}
  <Button variant={btnVariant} size="icon" class={btnClass} onclick={() => handleResetConnection(row.id)} title="Reset connection" aria-label="Reset connection" disabled={busy}>
    <Icon name={active('reset') ? 'refreshCw' : 'rotateCcw'} class={active('reset') ? `${iconSize} animate-spin` : iconSize} />
  </Button>
  {#if !isDefault}
    <Button variant="destructive" size="icon" class={btnClass} onclick={() => confirmDeleteConnection(row.id, row.name)} title="Delete connection" aria-label="Delete connection" disabled={busy}>
      <Icon name={active('delete') ? 'refreshCw' : 'trash2'} class={active('delete') ? `${iconSize} animate-spin` : iconSize} />
    </Button>
  {/if}
{/snippet}

<div class="flex flex-1 flex-col gap-6 p-6">
 <a href="/providers" class="inline-flex items-center gap-1.5 text-body-sm text-muted-foreground hover:text-foreground transition-colors w-fit">
 <Icon name="arrowLeft" class="size-3.5" />
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
 <Button onclick={() => { loadProvider(providerId); refreshConnections(); loadProviderModels(providerId); }} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
 </CardContent>
 </Card>
 {:else if $selectedProvider}
	<!-- Provider Header -->
	<div class="flex flex-col sm:flex-row items-start justify-between gap-4">
		<div class="flex items-start gap-4 min-w-0">
			<div
				class="size-12 rounded-lg flex items-center justify-center shrink-0 overflow-hidden"
				style="background-color: {(meta?.color ?? '#888')}15"
			>
				<ProviderIcon {meta} size={48} />
			</div>
			<div class="space-y-1 min-w-0">
				<div class="flex items-center gap-2 flex-wrap">
					<h1 class="text-display-lg break-words">{$selectedProvider.display_name}.</h1>
					{#if $selectedProvider.is_custom}
						<Badge variant="secondary" class="text-caption-mono rounded-sm">Custom</Badge>
					{/if}
					<Badge variant="outline" class="text-caption-mono rounded-sm">{providerCategoryLabel}</Badge>
					{#if providerServiceKinds.length > 0}
						{#each providerServiceKinds as kind (kind)}
							<Badge variant="secondary" class="text-caption-mono rounded-sm">{kind}</Badge>
						{/each}
					{/if}
				</div>
				<p class="text-caption-mono text-muted-foreground">
					Prefix: {meta?.prefix ?? ($selectedProvider.id + '/')} · Format: {$selectedProvider.format} · ID: {$selectedProvider.id}
				</p>
			</div>
		</div>
		{#if $selectedProvider.is_custom}
			<Button variant="outline" size="sm" class="text-body-sm rounded-sm gap-1.5 shrink-0" onclick={() => (showEditModal = true)}>
				<Icon name="pencil" class="size-3.5" /> Edit provider
			</Button>
		{/if}
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
 <Icon name="settings2" class="size-3.5" />
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

  {#if needsProxyPool && selectedCount > 0}
    <div class="flex items-center justify-between gap-3 flex-wrap rounded-xl border border-border bg-card shadow-card p-3">
      <span class="text-body-sm-strong">{selectedCount} selected</span>
      <div class="flex items-center gap-2 flex-wrap">
        <Select.Root
          type="single"
          value={selectedProxyPoolId}
          onValueChange={(value: string) => { selectedProxyPoolId = value; }}
        >
          <Select.Trigger class="w-[200px] h-9 text-body-sm rounded-sm">
            {selectedProxyPoolId ? (proxyPools.find((p) => p.id === selectedProxyPoolId)?.name ?? 'Select pool') : 'Unbind proxy pool'}
          </Select.Trigger>
          <Select.Content>
            <Select.Item value="" class="text-body-sm">Unbind proxy pool</Select.Item>
            {#each proxyPools.filter((p) => p.isActive) as pool (pool.id)}
              <Select.Item value={pool.id} class="text-body-sm">{pool.name}</Select.Item>
            {/each}
          </Select.Content>
        </Select.Root>
        <Button onclick={handleBulkAssignProxy} disabled={!canApplyProxy || bulkAssigning} size="sm" class="text-body-sm rounded-sm">
          {bulkAssigning ? 'Applying...' : 'Apply'}
        </Button>
        <Button onclick={clearSelection} variant="ghost" size="sm" class="text-body-sm rounded-sm">Clear</Button>
      </div>
    </div>
  {/if}

  <div class="flex flex-wrap gap-3">
  <Select.Root
  type="single"
  value={$connectionFilter.status}
 onValueChange={(value: string) => { $connectionFilter.status = value || ''; currentPage = 1; refreshConnections(); }}
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
 <Input type="text" class="w-full sm:w-64 h-9 text-body-sm" placeholder="Search connections..." bind:value={$connectionFilter.search} oninput={() => { currentPage = 1; refreshConnections(); }} />
 </div>

 <Card class="shadow-card overflow-hidden">
 <CardContent class="p-0">
    <!-- Desktop table -->
    <div class="hidden sm:block overflow-x-auto">
      <table class="w-full text-left border-collapse">
 <thead>
 <tr class="border-b border-border bg-muted/30">
 {#if needsProxyPool}
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-2 w-10 text-center">
   <input type="checkbox" checked={allVisibleSelected} onchange={toggleSelectAll} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select all connections" />
 </th>
 {/if}
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Name</th>
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Status</th>
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Auth</th>
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Failures</th>
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Cooldown</th>
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Last success</th>
 <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-40 text-right"></th>
 </tr>
 </thead>
<tbody class="divide-y divide-border">
          {#if $connections.length === 0}
          <tr><td colspan={needsProxyPool ? 8 : 7} class="p-0">
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
          <tr class="transition-colors hover:bg-accent/20 group">
            {#if needsProxyPool}
            <td class="py-3 px-2 text-center">
              <input type="checkbox" checked={selectedConnectionIds.has(row.id)} onchange={() => toggleSelectConnection(row.id)} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select {row.name}" />
            </td>
            {/if}
            <td class="py-3 px-4">
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                <a href="/providers/{providerId}/{row.id}" class="inline-flex items-center gap-1.5 text-body-sm-strong hover:underline">
                  <span class="size-2 rounded-full shrink-0" style="background-color: {getStatusDotColor(row.status)}"></span>
                  {row.name}
                </a>
                {@render connectionBadges(row)}
              </div>
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
              <div class="flex items-center justify-end gap-0.5">
                {@render connectionActions(row)}
              </div>
            </td>
          </tr>
          {/each}
          {/if}
        </tbody>
    </table>
    </div>

    <!-- Mobile card list -->
    <div class="sm:hidden divide-y divide-border">
      {#if $connections.length === 0}
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
      {:else}
        {#each $connections as row}
        <div class="p-4 space-y-3">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 space-y-1">
              <a href="/providers/{providerId}/{row.id}" class="inline-flex items-center gap-1.5 text-body-sm-strong hover:underline break-words">
                <span class="size-2 rounded-full shrink-0" style="background-color: {getStatusDotColor(row.status)}"></span>
                <span class="break-words">{row.name}</span>
              </a>
              {@render connectionBadges(row)}
            </div>
            <div class="flex items-center gap-2 shrink-0">
              {#if needsProxyPool}
                <input type="checkbox" checked={selectedConnectionIds.has(row.id)} onchange={() => toggleSelectConnection(row.id)} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select {row.name}" />
              {/if}
              <Button variant="ghost" size="icon" class="size-9 shrink-0" href={`/providers/${providerId}/${row.id}`} title="Open connection" aria-label="Open connection">
                <Icon name="chevronRight" class="size-6" />
              </Button>
            </div>
          </div>

          <div class="grid grid-cols-2 gap-3 text-caption-mono text-muted-foreground">
            <div>
              <p class="uppercase font-semibold text-[10px]">Status</p>
              <Badge variant={getStatusVariant(row.status)} class="mt-1 text-caption-mono rounded-sm py-0.5">
                {getStatusLabel(row.status)}
              </Badge>
            </div>
            <div>
              <p class="uppercase font-semibold text-[10px]">Auth</p>
              <p class="mt-1 text-body-sm font-mono text-foreground">{row.auth_type}</p>
            </div>
            <div>
              <p class="uppercase font-semibold text-[10px]">Failures</p>
              <p class="mt-1 text-body-sm font-mono {row.failure_count > 0 ? 'text-destructive font-semibold' : 'text-foreground'}">{row.failure_count}</p>
            </div>
            <div>
              <p class="uppercase font-semibold text-[10px]">Cooldown</p>
              <p class="mt-1 text-body-sm font-mono text-foreground">{formatCooldown(row.cooldown_until)}</p>
            </div>
            <div class="col-span-2">
              <p class="uppercase font-semibold text-[10px]">Last success</p>
              <p class="mt-1 text-body-sm font-mono text-foreground">{formatTimestamp(row.last_success_at)}</p>
            </div>
          </div>

          <div class="flex items-center gap-2 pt-3 border-t border-border">
            {@render connectionActions(row, 'size-9', 'outline', 'size-5')}
          </div>
        </div>
        {/each}
      {/if}
    </div>
  </CardContent>
</Card>

<Pagination
 page={currentPage}
 totalPages={$connectionPagination.total_pages}
 total={$connectionPagination.total}
 perPage={perPage}
 perPageOptions={[25, 50, 100, { value: 0, label: 'All' }]}
 onPerPageChange={handlePerPageChange}
 onChange={handlePageChange}
/>

 </div>

	<!-- Models Section (below connections) -->
	<div class="space-y-5">
		<div class="flex items-center justify-between gap-3 flex-wrap">
			<div class="space-y-1">
				<h2 class="text-display-md">Available Models</h2>
				<p class="text-body-sm text-muted-foreground">Names include the provider alias (e.g. oc/...) and match /v1/models. Click a model to test it.</p>
			</div>
			<div class="flex items-center gap-2">
				<Input bind:value={newModel} placeholder="Add custom model (e.g. my-model)" class="text-body-sm rounded-sm" />
				<Button onclick={async () => { if (!newModel.trim()) return; await addProviderModel(providerId, newModel.trim()); newModel = ''; }} variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer" disabled={!newModel.trim()}>Add model</Button>
			</div>
		</div>
		{#if $providerModels.length === 0}
			<p class="text-body-sm text-muted-foreground">No models registered for this provider yet.</p>
		{:else}
			{@const grouped = groupedProviderModels}
			{#each MODEL_KIND_ORDER as [kind, label] (kind)}
				{@const entries = grouped.get(kind) ?? []}
				{#if entries.length > 0}
					<div class="space-y-2">
						<h3 class="text-body-sm-strong text-foreground flex items-center gap-2">
							{label}
							<span class="text-caption-mono text-muted-foreground font-normal">{entries.length}</span>
						</h3>
						<div class="grid gap-2" style="grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));">
							{#each entries as entry (entry.id)}
								{@const result = $modelTestResults[entry.id]}
								{@const serviceKinds = entry.service_kinds?.length === 1 && entry.service_kinds[0] === 'llm' ? [] : (entry.service_kinds ?? [])}
								<div class="group relative flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 transition-colors hover:border-primary/40">
									<button
										type="button"
										class="flex min-w-0 flex-1 items-center gap-2 text-left cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
										disabled={result?.status === 'testing'}
										onclick={() => testProviderModel(providerId, entry.id)}
										title={entry.id}
									>
										<span class="block min-w-0 text-[12px] font-mono leading-snug break-all text-foreground">{entry.id}</span>
									</button>
									{#if serviceKinds.length > 0}
										<div class="flex flex-wrap gap-1">
											{#each serviceKinds as k (k)}
												<Badge variant="outline" class="text-[10px] px-1.5 py-0 rounded-full">{k}</Badge>
											{/each}
										</div>
									{/if}
									<button
										type="button"
										class="inline-flex shrink-0 items-center justify-center size-6 rounded-sm border border-border text-muted-foreground transition-colors hover:border-primary/50 hover:text-primary hover:bg-primary/10 cursor-pointer"
										title="Copy model name"
										aria-label="Copy model name"
										onclick={(e) => { e.stopPropagation(); copyModelName(entry.id); }}
									>
										<Icon name="copy" class="size-3" />
									</button>
									{#if result}
										<span class="size-1.5 shrink-0 rounded-full {result.status === 'ok' ? 'bg-emerald-500' : result.status === 'testing' ? 'bg-yellow-500 animate-pulse' : 'bg-destructive'}"></span>
										{#if result.latency_ms}
											<span class="shrink-0 text-[10px] font-mono text-muted-foreground">{result.latency_ms}ms</span>
										{/if}
									{/if}
									{#if entry.custom}
										<button
											type="button"
											class="ml-1 size-5 inline-flex shrink-0 items-center justify-center rounded-sm border border-border text-muted-foreground transition-colors hover:border-destructive/50 hover:text-destructive hover:bg-destructive/10 cursor-pointer"
											title="Remove custom model"
											aria-label="Remove custom model"
											onclick={() => deleteProviderModel(providerId, entry.id)}
										>×</button>
									{/if}
								</div>
							{/each}
						</div>
					</div>
				{/if}
			{/each}
		{/if}
	</div>

 {/if}
 </div>

<AddConnectionModal bind:open={showAddModal} {providerId} {meta} onCreated={() => { refreshConnections(); loadProvider(providerId); loadProviderModels(providerId); }} />
<ProviderRoutingModal bind:open={showRoutingModal} {providerId} currentMode={routingMode} onSaved={(mode) => (routingMode = mode)} />
<ProviderEditModal
	bind:open={showEditModal}
	{providerId}
	currentBaseUrl={$selectedProvider?.base_url ?? ''}
	currentDisplayName={$selectedProvider?.display_name ?? ''}
	currentServiceKinds={$selectedProvider?.service_kinds ?? []}
	currentFormat={$selectedProvider?.format ?? 'openai'}
	onSaved={() => loadProvider(providerId)}
/>

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
