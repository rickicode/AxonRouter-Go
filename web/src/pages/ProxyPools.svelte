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
import * as AlertDialog from '$lib/components/ui/alert-dialog';
import * as Tabs from '$lib/components/ui/tabs';
import Pagination from '$lib/components/Pagination.svelte';
  import StatusBadge from '$lib/components/StatusBadge.svelte';
  import { toast } from 'svelte-sonner';
import SearchIcon from '@lucide/svelte/icons/search';
import XIcon from '@lucide/svelte/icons/x';

let tab = $state<'pools' | 'groups' | 'assignments' | 'deploy'>('pools');
let pools = $state<ProxyPool[]>([]);
let allPools = $state<ProxyPool[]>([]);
let groups = $state<ProxyGroup[]>([]);
  let providers = $state<Provider[]>([]);
  let loading = $state(true);
  let error = $state('');
// Pools pagination
let poolPage = $state(1);
let poolPerPage = $state(10);
let poolTotal = $state(0);
let poolTotalPages = $state(1);

// Pools filtering
let poolSearch = $state('');
let poolTypeFilter = $state('');
let searchTimeout: ReturnType<typeof setTimeout> | undefined;

const hasPoolFilters = $derived(poolSearch.trim() !== '' || poolTypeFilter !== '');

// Modal state
let showAddPool = $state(false);
let addPoolTab = $state<'single' | 'bulk'>('single');
let showGroupModal = $state(false);
let showEditPool = $state(false);
let showDeleteErrorConfirm = $state(false);
let editPoolId = $state('');
let editPoolName = $state('');
let editPoolUrl = $state('');
let editPoolType = $state('http');
let editPoolNoProxy = $state('');
let editPoolLoading = $state(false);

// Create pool form
let poolName = $state('');
let poolUrl = $state('');
let poolType = $state('http');
let poolNoProxy = $state('');
let createPoolLoading = $state(false);

// Bulk import form
let bulkText = $state('');
let bulkType = $state('http');
let bulkNoProxy = $state('');
let bulkActive = $state(true);
let bulkHealthy = $state(false);
let bulkSub1s = $state(false);
let bulkLoading = $state(false);
// .txt upload + chunked send (keeps RAM bounded, mirrors provider bulk import).
const POOL_BULK_CHUNK = 1000;
const POOL_BULK_CONCURRENCY = 3;
let uploadedPoolFile = $state('');
let parsedPoolItems = $state<string[]>([]);
let poolParseWarnings = $state<string[]>([]);
let poolImportProgress = $state(0); // 0..1
let poolImportSummary = $state<{ created: number; skipped: number; errors: number } | null>(null);

function parsePoolLines(text: string): { items: string[]; warnings: string[] } {
  const lines = text.split('\n').map((l) => l.trim()).filter((l) => l.length > 0 && !l.startsWith('#'));
  const items: string[] = [];
  const warnings: string[] = [];
  lines.forEach((line, index) => {
    const normalized = normalizeBulkLine(line);
    if (!normalized) {
      warnings.push(`Line ${index + 1}: empty after normalization`);
      return;
    }
    items.push(normalized);
  });
  return { items, warnings };
}

function handlePoolFileUpload(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  uploadedPoolFile = file.name;
  poolImportSummary = null;
  const reader = new FileReader();
  reader.onload = () => {
    const text = typeof reader.result === 'string' ? reader.result : '';
    const { items, warnings } = parsePoolLines(text);
    parsedPoolItems = items;
    poolParseWarnings = warnings;
    if (items.length === 0) toast.error('No valid proxy URLs found in file');
    else if (warnings.length > 0) toast.info(`${items.length} valid, ${warnings.length} invalid line(s) skipped`);
    else toast.success(`Parsed ${items.length} proxy URL(s)`);
  };
  reader.onerror = () => toast.error('Failed to read file');
  reader.readAsText(file);
  input.value = '';
}

async function handleBulkImportChunked() {
  const items = parsedPoolItems.length > 0 ? parsedPoolItems : bulkText.trim().split('\n').map((l) => l.trim()).filter((l) => l.length > 0 && !l.startsWith('#')).map(normalizeBulkLine);
  if (items.length === 0) {
    toast.error('Paste or upload at least one proxy URL');
    return;
  }
  bulkLoading = true;
  poolImportProgress = 0;
  poolImportSummary = { created: 0, skipped: 0, errors: 0 };

  // Build the batch queue (each batch <= backend maxBulkItems=1000).
  const queue: string[][] = [];
  for (let start = 0; start < items.length; start += POOL_BULK_CHUNK) {
    queue.push(items.slice(start, start + POOL_BULK_CHUNK));
  }

  let next = 0;
  let done = 0;
  let aborted = false;
  const worker = async () => {
    while (!aborted) {
      const i = next++;
      if (i >= queue.length) break;
      try {
        const res = await proxyPoolsApi.bulkCreate({
          items: queue[i],
          defaultType: bulkType,
          noProxy: bulkNoProxy.trim() || undefined,
          isActive: bulkActive,
          requireHealthy: bulkHealthy,
          maxResponseTimeMs: bulkHealthy && bulkSub1s ? 1000 : undefined,
        });
        poolImportSummary!.created += res.created ?? 0;
        poolImportSummary!.skipped += res.skipped ?? 0;
        poolImportSummary!.errors += res.errors ?? 0;
      } catch (err) {
        aborted = true;
        throw err;
      } finally {
        done++;
        poolImportProgress = done / queue.length;
      }
    }
  };

  try {
    await Promise.all(Array.from({ length: Math.min(POOL_BULK_CONCURRENCY, queue.length) }, () => worker()));
    const s = poolImportSummary;
    const msg = `${s.created} created, ${s.skipped} skipped, ${s.errors} errors`;
    if (s.errors === 0) toast.success('Bulk import complete', { description: msg });
    else toast.error('Bulk import finished with errors', { description: msg });
    bulkText = '';
    parsedPoolItems = [];
    uploadedPoolFile = '';
    poolParseWarnings = [];
    poolPage = 1;
    await loadAll(true);
  } catch (err) {
    toast.error('Bulk import failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    bulkLoading = false;
  }
}

// Selection
let selectedPoolIds = $state<Set<string>>(new Set());
const selectedCount = $derived(selectedPoolIds.size);
const allVisibleSelected = $derived(pools.length > 0 && pools.every(p => selectedPoolIds.has(p.id)));
function hasScheme(url: string): boolean {
  return /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(url);
}

function normalizeBulkLine(line: string): string {
  const idx = line.indexOf('|');
  if (idx > 0) {
    const name = line.slice(0, idx).trim();
    let proxyUrl = line.slice(idx + 1).trim();
    if (bulkType === 'http' && proxyUrl && !hasScheme(proxyUrl)) {
      proxyUrl = 'http://' + proxyUrl;
    }
    return `${name}|${proxyUrl}`;
  }
  if (bulkType === 'http' && line && !hasScheme(line)) {
    return 'http://' + line;
  }
  return line;
}


// Create/Edit group form
let groupName = $state('');
let groupMode = $state('roundrobin');
let groupStickyLimit = $state(1);
let groupStrict = $state(false);
let editGroupId = $state('');
let groupModalSaving = $state(false);
let groupModalTesting = $state(false);
let groupModalPools = $state<ProxyPool[]>([]);
let groupModalSelectedIds = $state<Set<string>>(new Set());
let groupModalLoading = $state(false);
const groupModalSelectedCount = $derived(groupModalSelectedIds.size);
const allGroupModalSelected = $derived(groupModalPools.length > 0 && groupModalPools.every(p => groupModalSelectedIds.has(p.id)));

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
const enabledCount = $derived(allPools.filter(p => p.isActive).length);
const onlineCount = $derived(allPools.filter(p => p.testStatus === 'active').length);
const errorCount = $derived(allPools.filter(p => p.testStatus === 'error').length);

  onMount(() => {
    document.title = 'Proxy Pools — AxonRouter';
    loadAll();
  });

async function loadPools() {
  try {
    const params: Record<string, string> = { page: String(poolPage), per_page: String(poolPerPage) };
    if (poolTypeFilter) params.type = poolTypeFilter;
    if (poolSearch.trim()) params.q = poolSearch.trim();
    const res = await proxyPoolsApi.list(params);
    pools = res.data ?? [];
    poolTotal = res.pagination?.total ?? 0;
    poolTotalPages = res.pagination?.total_pages ?? 1;
  } catch (err) {
    toast.error('Failed to load pools: ' + (err instanceof Error ? err.message : 'Unknown'));
  }
}

function applyPoolFilters(page = 1) {
  poolPage = page;
  selectedPoolIds = new Set();
  void loadPools();
}

function onPoolSearchInput() {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => applyPoolFilters(1), 300);
}

function onPoolTypeChange(val: string) {
  poolTypeFilter = val;
  applyPoolFilters(1);
}

function clearPoolFilters() {
  poolSearch = '';
  poolTypeFilter = '';
  applyPoolFilters(1);
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
    allPools = await proxyPoolsApi.listAll();
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
		showAddPool = false;
		poolName = ''; poolUrl = ''; poolNoProxy = '';
		poolPage = 1;
		await loadAll(true);
	} catch (err) { toast.error(err instanceof Error ? err.message : 'Unknown error'); }
	finally { createPoolLoading = false; }
}

function resetAddPoolModal(tab: 'single' | 'bulk') {
	addPoolTab = tab;
	poolName = '';
	poolUrl = '';
	poolNoProxy = '';
	poolType = 'http';
	bulkText = '';
	bulkType = 'http';
	bulkNoProxy = '';
	bulkActive = true;
	bulkHealthy = false;
	bulkSub1s = false;
}

function toggleSelectPool(id: string) {
	if (selectedPoolIds.has(id)) {
		selectedPoolIds.delete(id);
	} else {
		selectedPoolIds.add(id);
	}
	selectedPoolIds = new Set(selectedPoolIds);
}

function toggleSelectAll() {
	if (allVisibleSelected) {
		for (const p of pools) selectedPoolIds.delete(p.id);
	} else {
		for (const p of pools) selectedPoolIds.add(p.id);
	}
	selectedPoolIds = new Set(selectedPoolIds);
}

function selectErrorPools() {
	for (const p of pools) {
		if (p.testStatus === 'error') selectedPoolIds.add(p.id);
	}
	selectedPoolIds = new Set(selectedPoolIds);
}

function clearSelection() {
	selectedPoolIds = new Set();
}

async function testSelectedPools() {
	try {
		await Promise.all([...selectedPoolIds].map(id => proxyPoolsApi.test(id)));
		toast.success(`Tested ${selectedPoolIds.size} pools`);
	} catch (err) {
		toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		selectedPoolIds = new Set();
		await loadAll(true);
	}
}

async function deleteSelectedPools() {
	try {
		const res = await proxyPoolsApi.bulkDelete({ ids: [...selectedPoolIds] });
		toast.success(`Deleted ${res.deleted} pools`);
	} catch (err) {
		toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		selectedPoolIds = new Set();
		await loadAll(true);
	}
}

async function deleteAllErrorPools() {
	try {
		const res = await proxyPoolsApi.bulkDelete({ status: 'error' });
		toast.success(`Deleted ${res.deleted} error pools`);
	} catch (err) {
		toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		showDeleteErrorConfirm = false;
		selectedPoolIds = new Set();
		await loadAll(true);
	}
}

// --- Pool edit (inline from the list) ---
function openEditPool(pool: ProxyPool) {
	editPoolId = pool.id;
	editPoolName = pool.name;
	editPoolUrl = pool.proxyUrl;
	editPoolType = pool.type;
	editPoolNoProxy = pool.noProxy ?? '';
	showEditPool = true;
}

async function handleEditPool() {
	if (!editPoolId || !editPoolName.trim() || !editPoolUrl.trim()) return;
	editPoolLoading = true;
	try {
		await proxyPoolsApi.update(editPoolId, {
			name: editPoolName.trim(),
			proxyUrl: editPoolUrl.trim(),
			type: editPoolType,
			noProxy: editPoolNoProxy.trim() || undefined,
		});
		toast.success('Proxy pool updated');
		showEditPool = false;
		await loadAll(true);
	} catch (err) { toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
	finally { editPoolLoading = false; }
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
function resetGroupForm() {
  groupName = '';
  groupMode = 'roundrobin';
  groupStickyLimit = 1;
  groupStrict = false;
  editGroupId = '';
  groupModalSelectedIds = new Set();
}

async function loadGroupModalPools() {
  groupModalLoading = true;
  try {
    groupModalPools = await proxyPoolsApi.listAll();
  } catch (err) {
    toast.error('Failed to load pools: ' + (err instanceof Error ? err.message : 'Unknown'));
    groupModalPools = [];
  } finally {
    groupModalLoading = false;
  }
}

function openCreateGroup() {
  resetGroupForm();
  showGroupModal = true;
  loadGroupModalPools();
}

function openEditGroup(group: ProxyGroup) {
  resetGroupForm();
  editGroupId = group.id;
  groupName = group.name;
  groupMode = group.mode;
  groupStickyLimit = group.stickyLimit ?? 1;
  groupStrict = group.strictProxy ?? false;
  groupModalSelectedIds = new Set(group.proxyPoolIds ?? []);
  showGroupModal = true;
  loadGroupModalPools();
}

async function handleSaveGroup() {
  if (!groupName.trim()) return;
  groupModalSaving = true;
  try {
    const payload = {
      name: groupName.trim(),
      mode: groupMode,
      stickyLimit: groupStickyLimit,
      strictProxy: groupStrict,
      proxyPoolIds: [...groupModalSelectedIds],
    };
    if (!editGroupId) {
      await proxyGroupsApi.create({ ...payload, isActive: true });
      toast.success('Proxy group created');
    } else {
      await proxyGroupsApi.update(editGroupId, payload);
      toast.success('Group updated');
    }
    showGroupModal = false;
    resetGroupForm();
    await loadAll(true);
  } catch (err) {
    toast.error((editGroupId ? 'Update' : 'Create') + ' failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    groupModalSaving = false;
  }
}

function toggleGroupModalPool(poolId: string) {
  const s = new Set(groupModalSelectedIds);
  if (s.has(poolId)) s.delete(poolId);
  else s.add(poolId);
  groupModalSelectedIds = s;
}

function toggleGroupModalSelectAll() {
  const s = new Set(groupModalSelectedIds);
  if (allGroupModalSelected) {
    for (const p of groupModalPools) s.delete(p.id);
  } else {
    for (const p of groupModalPools) s.add(p.id);
  }
  groupModalSelectedIds = s;
}

function clearGroupModalSelection() {
  groupModalSelectedIds = new Set();
}

function selectHealthyGroupModalPools() {
  const s = new Set(groupModalSelectedIds);
  for (const p of groupModalPools) {
    if (p.testStatus === 'active') s.add(p.id);
  }
  groupModalSelectedIds = s;
  const count = groupModalPools.filter(p => p.testStatus === 'active').length;
  toast.success(`Selected ${count} healthy pool${count === 1 ? '' : 's'}`);
}

function selectLowestLatencyGroupModalPool() {
  let best: ProxyPool | null = null;
  let bestMs: number | null = null;
  for (const p of groupModalPools) {
    if (p.responseTimeMs == null) continue;
    if (bestMs == null || p.responseTimeMs < bestMs) {
      best = p;
      bestMs = p.responseTimeMs;
    }
  }
  if (best && bestMs != null) {
    groupModalSelectedIds = new Set([best.id]);
    toast.success(`Selected ${best.name} (${bestMs}ms)`);
  } else {
    toast.info('No pools with latency data');
  }
}

async function testGroupModalSelectedPools() {
  if (groupModalSelectedIds.size === 0) return;
  groupModalTesting = true;
  try {
    let ok = 0;
    let failed = 0;
    await Promise.all([...groupModalSelectedIds].map(async (id) => {
      try {
        const res = await proxyPoolsApi.test(id);
        if (res.ok) ok++;
        else failed++;
      } catch {
        failed++;
      }
    }));
    await loadGroupModalPools();
    await loadAll(true);
    toast.success(`Tested ${ok + failed} pool${ok + failed === 1 ? '' : 's'} (${ok} OK, ${failed} failed)`);
  } catch (err) {
    toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    groupModalTesting = false;
  }
}

async function runGroupModalHealthCheck() {
  try {
    const res = await proxyPoolsApi.healthRun();
    toast.success(`Health check done (${res.results?.length ?? 0} pools)`);
    await loadGroupModalPools();
    await loadAll(true);
  } catch (err) {
    toast.error('Health check failed: ' + (err instanceof Error ? err.message : 'Unknown'));
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
  await handleBulkImportChunked();
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
        <Button onclick={() => loadAll()} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {/if}
  {#if !loading && !error}
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Proxy Pools.</h1>
        <div class="flex items-center gap-3 text-body-sm text-muted-foreground">
          <span>{poolTotal} pools</span>
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
				<Button onclick={runHealthCheck} variant="outline" class="text-body-sm rounded-sm px-4">
					Health check
				</Button>
				{#if tab === 'pools'}
					<Button onclick={() => { showDeleteErrorConfirm = true; }} variant="outline" size="sm" class="text-body-sm rounded-sm text-destructive px-4">
						Delete all error
					</Button>
					<Button onclick={() => { resetAddPoolModal('single'); showAddPool = true; }} class="text-button-md rounded-sm px-5">Add pool</Button>
				{:else if tab === 'groups'}
<Button onclick={openCreateGroup} class="text-button-md rounded-sm px-5">Add group</Button>
				{/if}
			</div>
    </div>

    <Tabs.Root bind:value={tab} class="w-full flex flex-col gap-6">
      <Tabs.List class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
        <Tabs.Trigger value="pools" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Pools ({poolTotal})</Tabs.Trigger>
        <Tabs.Trigger value="groups" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Groups ({groups.length})</Tabs.Trigger>
        <Tabs.Trigger value="assignments" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Assignments</Tabs.Trigger>
        <Tabs.Trigger value="deploy" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Deploy</Tabs.Trigger>
      </Tabs.List>

<!-- Pool Table -->
  <Tabs.Content value="pools">
  <section class="rounded-xl bg-card p-4 shadow-card md:p-5 mb-4">
  <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
    <div class="relative w-full md:max-w-md">
      <SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
      <Input
        type="text"
        bind:value={poolSearch}
        oninput={onPoolSearchInput}
        placeholder="Search pools…"
        class="h-10 pl-9 text-body-sm"
      />
      {#if poolSearch}
        <button
          type="button"
          class="absolute inset-y-0 right-2 text-caption text-muted-foreground hover:text-foreground cursor-pointer"
          onclick={() => { poolSearch = ''; applyPoolFilters(1); }}
        >Clear</button>
      {/if}
    </div>
    <div class="flex items-center gap-2">
      {#if hasPoolFilters}
        <Button variant="outline" size="sm" onclick={clearPoolFilters} class="gap-1 text-body-sm rounded-sm cursor-pointer">
          <XIcon class="size-3.5" /> Clear
        </Button>
      {/if}
      <Select.Root type="single" value={poolTypeFilter} onValueChange={onPoolTypeChange}>
        <Select.Trigger class="h-10 w-[150px] text-body-sm rounded-sm cursor-pointer">
          {poolTypeFilter ? typeLabel(poolTypeFilter) : 'All types'}
        </Select.Trigger>
        <Select.Content>
          <Select.Item value="" class="text-body-sm">All types</Select.Item>
          {#each typeOptions as opt}
            <Select.Item value={opt} class="text-body-sm">{typeLabel(opt)}</Select.Item>
          {/each}
        </Select.Content>
      </Select.Root>
    </div>
  </div>
</section>
{#if selectedCount > 0}
				<div class="flex items-center justify-between rounded-xl border border-border bg-card shadow-card p-3 mb-4">
					<span class="text-body-sm-strong">{selectedCount} selected</span>
					<div class="flex gap-2">
						<Button onclick={testSelectedPools} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Test selected</Button>
						<Button onclick={deleteSelectedPools} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Delete selected</Button>
						<Button onclick={selectErrorPools} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Select error</Button>
						<Button onclick={clearSelection} variant="ghost" size="sm" class="text-body-sm rounded-sm px-3">Clear</Button>
					</div>
				</div>
			{/if}
			{#if pools.length > 0}
				<Card class="shadow-card overflow-hidden p-0">
          <table class="w-full text-body-sm">
<thead>
						<tr class="border-b border-border bg-muted/50">
							<th class="text-left px-4 py-2.5 w-10">
								<input type="checkbox" checked={allVisibleSelected} onchange={toggleSelectAll} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select all pools" />
							</th>
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
						<tr class="border-b border-border hover:bg-muted/50 transition-colors">
							<td class="px-4 py-2.5">
								<input type="checkbox" checked={selectedPoolIds.has(pool.id)} onchange={() => toggleSelectPool(pool.id)} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select {pool.name}" />
							</td>
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
              <div class="flex justify-center">
                <Switch checked={pool.isActive} onCheckedChange={() => togglePoolActive(pool)} aria-label={pool.isActive ? 'Disable pool' : 'Enable pool'} />
              </div>
            </td>
                  <td class="px-4 py-2.5 text-center">
                    {#if pool.testStatus === 'active'}
                        <StatusBadge status="healthy" label="Online" />
                      {:else if pool.testStatus === 'error'}
                        <StatusBadge status="error" title={pool.lastError || ''} label="Error" />
                      {:else}
                        <StatusBadge status="idle" label="—" />
                      {/if}
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <span class="text-caption-mono {pool.responseTimeMs != null && pool.responseTimeMs < 500 ? 'text-emerald-400' : pool.responseTimeMs != null && pool.responseTimeMs < 2000 ? 'text-yellow-400' : 'text-muted-foreground'}">
                      {pool.responseTimeMs != null ? pool.responseTimeMs + 'ms' : '—'}
                    </span>
                  </td>
                  <td class="px-4 py-2.5 text-right">
			<div class="flex gap-1 justify-end">
				<Button onclick={() => openEditPool(pool)} variant="ghost" size="sm" class="text-caption-mono h-6 px-2 rounded-sm">Edit</Button>
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
					<Button onclick={() => { resetAddPoolModal('single'); showAddPool = true; }} class="text-button-md rounded-sm px-5">Add pool</Button>
          </CardContent>
        </Card>
      {/if}
      </Tabs.Content>

    <!-- Group Table -->
      <Tabs.Content value="groups">
      {#if groups.length > 0}
        <Card class="shadow-card overflow-hidden p-0">
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-border bg-muted/50">
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
                <tr class="border-b border-border hover:bg-muted/50 transition-colors">
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
              <div class="flex justify-center">
                <Switch checked={group.isActive} onCheckedChange={() => toggleGroupActive(group)} aria-label={group.isActive ? 'Disable group' : 'Enable group'} />
              </div>
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
<Button onclick={openCreateGroup} class="text-button-md rounded-sm px-5">Add group</Button>
      </CardContent>
    </Card>
    {/if}
  </Tabs.Content>

      <Tabs.Content value="assignments">
{#if providers.some(p => p.id !== 'oc') && pools.length > 0}
        <Card class="shadow-card overflow-hidden p-0">
          <div class="flex items-center justify-between px-4 py-3 border-b border-border bg-muted/50">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Provider → Proxy Assignment</p>
            <Button onclick={saveProxyDefaults} disabled={proxySaving} size="sm" class="text-body-sm rounded-sm">
              {proxySaving ? 'Saving...' : 'Save'}
            </Button>
          </div>
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-border">
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Provider</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Proxy Group</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Proxy Pool</th>
            </tr>
            </thead>
            <tbody>
              {#each providers.filter(p => p.id !== 'oc') as prov}
                <tr class="border-b border-border hover:bg-muted/50 transition-colors">
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
      </Tabs.Content>

        <!-- Deploy Tab -->
      <Tabs.Content value="deploy">
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
					<Button onclick={handleDeploy} disabled={deployLoading || !deployToken.trim()} class="text-button-md rounded-sm px-5">
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
      </Tabs.Content>
    </Tabs.Root>
  {/if}
</div>

<!-- Add Pool Dialog -->
<Dialog.Root bind:open={showAddPool}>
	<Dialog.Content class="sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title class="text-body-md-strong">Add proxy pool</Dialog.Title>
			<Dialog.Description class="text-xs">Create a single proxy pool or import many at once.</Dialog.Description>
		</Dialog.Header>
		<Tabs.Root bind:value={addPoolTab} class="w-full">
			<Tabs.List class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1 mb-2">
				<Tabs.Trigger value="single" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Single</Tabs.Trigger>
				<Tabs.Trigger value="bulk" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Bulk</Tabs.Trigger>
			</Tabs.List>
			<Tabs.Content value="single">
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
					<Button variant="ghost" onclick={() => (showAddPool = false)}>Cancel</Button>
								<Button onclick={handleCreatePool} disabled={createPoolLoading || !poolName.trim() || !poolUrl.trim()}>
									{createPoolLoading ? 'Checking proxy…' : 'Create pool'}
								</Button>
				</Dialog.Footer>
			</Tabs.Content>
  <Tabs.Content value="bulk">
    <div class="space-y-4 py-2">
      <div class="space-y-2">
        <Label class="text-sm font-medium">Proxy URLs</Label>
        <Textarea bind:value={bulkText} placeholder="http://user:pass@proxy:8080 or my-relay|https://relay.vercel.app" rows={10} class="text-body-sm font-mono w-full" />
      </div>

      <div class="flex items-center gap-3">
        <div class="h-px flex-1 bg-border"></div>
        <span class="text-[11px] uppercase tracking-wide text-muted-foreground">or upload</span>
        <div class="h-px flex-1 bg-border"></div>
      </div>

      <div class="flex flex-col gap-2">
        <label
          class="flex cursor-pointer items-center justify-center gap-2 rounded-lg border border-dashed border-border bg-muted/30 px-4 py-3 text-sm text-muted-foreground transition-colors hover:border-primary/50 hover:text-foreground"
          class:opacity-50={bulkLoading}
        >
          <svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v2a2 2 0 002 2h12a2 2 0 002-2v-2M12 15V3m0 0L8 7m4-4l4 4" /></svg>
          <span>{uploadedPoolFile ? uploadedPoolFile : 'Upload .txt file'}</span>
          <input
            type="file"
            accept=".txt,text/plain"
            class="hidden"
            disabled={bulkLoading}
            onchange={handlePoolFileUpload}
          />
        </label>
        {#if uploadedPoolFile}
          <div class="flex items-center justify-between rounded-md bg-card px-3 py-2 text-xs">
            <span class="text-foreground">{parsedPoolItems.length} proxy URL(s) parsed</span>
            <button
              type="button"
              class="text-muted-foreground underline-offset-2 hover:text-destructive hover:underline"
              onclick={() => { uploadedPoolFile = ''; parsedPoolItems = []; poolParseWarnings = []; poolImportSummary = null; }}
            >
              Clear
            </button>
          </div>
        {/if}
        {#if poolParseWarnings.length > 0}
          <div class="max-h-24 overflow-y-auto rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-[11px] text-amber-400">
            {#each poolParseWarnings as w}
              <div>{w}</div>
            {/each}
          </div>
        {/if}
      </div>

      <div class="space-y-4">
        <div class="space-y-2">
          <Label class="text-sm font-medium">Default type</Label>
          <div class="inline-flex w-fit flex-wrap items-center gap-1 rounded-lg bg-muted p-1">
            {#each typeOptions as opt}
            <button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {bulkType === opt ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (bulkType = opt)}>{typeLabel(opt)}</button>
            {/each}
          </div>
        </div>
        <div class="space-y-2">
          <Label class="text-sm font-medium">No Proxy (optional)</Label>
          <Input bind:value={bulkNoProxy} placeholder="localhost,127.0.0.1" class="h-10 text-body-sm font-mono w-full" />
        </div>
        <div class="flex items-center gap-3">
          <Switch id="bulk-active" checked={bulkActive} onCheckedChange={(v) => (bulkActive = v)} />
          <Label for="bulk-active" class="text-sm font-medium cursor-pointer">Active after import</Label>
        </div>
        <div class="flex items-center gap-3">
          <Switch id="bulk-healthy" checked={bulkHealthy} onCheckedChange={(v) => (bulkHealthy = v)} />
          <Label for="bulk-healthy" class="text-sm font-medium cursor-pointer">Only import healthy proxies</Label>
        </div>
        <div class="flex items-center gap-3">
          <Switch id="bulk-sub1s" checked={bulkSub1s} onCheckedChange={(v) => (bulkSub1s = v)} disabled={!bulkHealthy} />
          <Label for="bulk-sub1s" class="text-sm font-medium cursor-pointer {bulkHealthy ? '' : 'text-muted-foreground'}">&lt;1s response</Label>
        </div>
      </div>

      {#if bulkLoading}
        <div class="flex flex-col gap-1.5">
          <div class="h-2 w-full overflow-hidden rounded-full bg-muted">
            <div
              class="h-full rounded-full bg-primary transition-all duration-300"
              style="width: {Math.round(poolImportProgress * 100)}%"
            ></div>
          </div>
          <p class="text-[11px] text-muted-foreground">
            Importing… {Math.round(poolImportProgress * 100)}%
            {#if poolImportSummary}
              ({poolImportSummary.created + poolImportSummary.skipped + poolImportSummary.errors})
            {/if}
          </p>
        </div>
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => (showAddPool = false)}>Cancel</Button>
      <Button onclick={handleBulkImport} disabled={bulkLoading || (!bulkText.trim() && parsedPoolItems.length === 0)}>
        {bulkLoading ? `Importing… ${Math.round(poolImportProgress * 100)}%` : 'Import pools'}
      </Button>
    </Dialog.Footer>
  </Tabs.Content>
		</Tabs.Root>
	</Dialog.Content>
</Dialog.Root>

<!-- Delete all error confirmation -->
<AlertDialog.Root bind:open={showDeleteErrorConfirm}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete all error pools?</AlertDialog.Title>
			<AlertDialog.Description>
				This will permanently delete every proxy pool with health status "error". This action cannot be undone.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel onclick={() => (showDeleteErrorConfirm = false)}>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action onclick={deleteAllErrorPools} class="bg-destructive text-destructive-foreground hover:bg-destructive/90">Delete all</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<!-- Group Dialog -->
<Dialog.Root bind:open={showGroupModal}>
  <Dialog.Content class="sm:max-w-6xl max-h-[90vh] overflow-hidden flex flex-col">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">{editGroupId ? 'Edit proxy group' : 'Create proxy group'}</Dialog.Title>
      <Dialog.Description class="text-xs">{editGroupId ? 'Update routing mode, strict proxy, and pool membership.' : 'Combine multiple pools with round-robin, sticky, or random routing.'}</Dialog.Description>
    </Dialog.Header>
    <div class="space-y-4 overflow-y-auto pr-1">
<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div class="space-y-2">
					<Label class="text-sm font-medium">Name</Label>
					<Input bind:value={groupName} placeholder="e.g. us-proxies" class="h-10 text-body-sm" />
				</div>
				<div class="space-y-2">
					<Label class="text-sm font-medium">Mode</Label>
					<div class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
						<button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {groupMode === 'roundrobin' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'roundrobin')}>Round Robin</button>
						<button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {groupMode === 'sticky' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'sticky')}>Sticky</button>
						<button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {groupMode === 'random' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (groupMode = 'random')}>Random</button>
					</div>
				</div>
			</div>
			{#if groupMode === 'sticky'}
				<div class="space-y-2 max-w-[140px]">
					<Label class="text-sm font-medium">Sticky Limit</Label>
					<Input type="number" bind:value={groupStickyLimit} min={1} class="h-10 text-code font-mono" />
				</div>
			{/if}
<div class="flex items-center gap-3">
			<Switch id="group-strict" checked={groupStrict} onCheckedChange={(v) => (groupStrict = v)} />
			<Label for="group-strict" class="text-sm font-medium cursor-pointer">Strict proxy</Label>
		</div>
      <div class="space-y-2">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <Label class="text-sm font-medium">Pools ({groupModalSelectedCount} selected)</Label>
          <div class="flex flex-wrap gap-2">
            <Button onclick={toggleGroupModalSelectAll} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">{allGroupModalSelected ? 'Deselect all' : 'Select all'}</Button>
            <Button onclick={clearGroupModalSelection} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Clear</Button>
            <Button onclick={testGroupModalSelectedPools} disabled={groupModalTesting || groupModalSelectedCount === 0} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">{groupModalTesting ? 'Testing...' : 'Test selected'}</Button>
            <Button onclick={runGroupModalHealthCheck} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Test all</Button>
            <Button onclick={selectLowestLatencyGroupModalPool} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Select lowest latency</Button>
            <Button onclick={selectHealthyGroupModalPools} variant="outline" size="sm" class="text-body-sm rounded-sm px-3">Select healthy</Button>
          </div>
        </div>
        {#if groupModalLoading}
        <div class="h-32 bg-muted animate-pulse rounded-xl"></div>
        {:else if groupModalPools.length > 0}
        <Card class="shadow-card overflow-hidden p-0">
          <div class="max-h-[360px] overflow-y-auto">
            <table class="w-full text-body-sm">
              <thead class="sticky top-0 z-10">
                <tr class="border-b border-border bg-muted/50">
                  <th class="text-left px-4 py-2.5 w-10">
                    <input type="checkbox" checked={allGroupModalSelected} onchange={toggleGroupModalSelectAll} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select all pools" />
                  </th>
                  <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Name</th>
                  <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Proxy URL</th>
                  <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Type</th>
                  <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Health</th>
                  <th class="text-right text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Latency</th>
                  <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Location / ISP</th>
                </tr>
              </thead>
              <tbody>
                {#each groupModalPools as pool}
                <tr class="border-b border-border hover:bg-muted/50 transition-colors">
                  <td class="px-4 py-2.5">
                    <input type="checkbox" checked={groupModalSelectedIds.has(pool.id)} onchange={() => toggleGroupModalPool(pool.id)} class="size-4 rounded border-border bg-background text-foreground accent-foreground cursor-pointer" aria-label="Select {pool.name}" />
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-body-sm-strong truncate block max-w-[160px]">{pool.name}</span>
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-caption-mono text-muted-foreground truncate block max-w-[260px]">{pool.proxyUrl}</span>
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="text-caption-mono text-muted-foreground">{typeLabel(pool.type)}</span>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    {#if pool.testStatus === 'active'}
                    <StatusBadge status="healthy" label="Online" />
                    {:else if pool.testStatus === 'error'}
                    <StatusBadge status="error" title={pool.lastError || ''} label="Error" />
                    {:else}
                    <StatusBadge status="idle" label="—" />
                    {/if}
                  </td>
                  <td class="px-4 py-2.5 text-right">
                    <span class="text-caption-mono {pool.responseTimeMs != null && pool.responseTimeMs < 500 ? 'text-emerald-400' : pool.responseTimeMs != null && pool.responseTimeMs < 2000 ? 'text-yellow-400' : 'text-muted-foreground'}">
                      {pool.responseTimeMs != null ? pool.responseTimeMs + 'ms' : '—'}
                    </span>
                  </td>
                  <td class="px-4 py-2.5">
                    {#if pool.proxyCountry || pool.proxyIp}
                    <span class="text-[10px] text-muted-foreground/60 truncate block max-w-[220px]" title={pool.proxyIp || ''}>
                      {pool.proxyCountry || '—'}{pool.proxyCity ? ', ' + pool.proxyCity : ''}{pool.proxyOrg ? ' • ' + pool.proxyOrg.replace(/^AS\d+\s*/, '') : ''}
                    </span>
                    {:else}
                    <span class="text-muted-foreground text-[10px]">—</span>
                    {/if}
                  </td>
                </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </Card>
        {:else}
        <Card class="shadow-card">
          <CardContent class="flex flex-col items-center justify-center py-12">
            <p class="text-body-sm text-muted-foreground">No proxy pools available.</p>
          </CardContent>
        </Card>
        {/if}
      </div>
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => (showGroupModal = false)}>Cancel</Button>
      <Button onclick={handleSaveGroup} disabled={groupModalSaving || !groupName.trim()}>
        {groupModalSaving ? 'Saving...' : (editGroupId ? 'Save' : 'Create')}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>



<!-- Edit Pool Dialog -->
	<Dialog.Root bind:open={showEditPool}>
		<Dialog.Content class="sm:max-w-lg">
			<Dialog.Header>
				<Dialog.Title class="text-body-md-strong">Edit proxy pool</Dialog.Title>
				<Dialog.Description class="text-xs">Update the name, endpoint, type, or no-proxy list for this pool.</Dialog.Description>
			</Dialog.Header>
			<div class="space-y-4 py-2">
				<div class="space-y-2">
					<Label class="text-sm font-medium">Name</Label>
					<Input bind:value={editPoolName} class="h-10 text-body-sm" />
				</div>
				<div class="space-y-2">
					<Label class="text-sm font-medium">Proxy URL</Label>
					<Input bind:value={editPoolUrl} class="h-10 text-body-sm font-mono" />
				</div>
				<div class="space-y-2">
					<Label class="text-sm font-medium">Type</Label>
					<div class="inline-flex w-fit flex-wrap items-center gap-1 rounded-lg bg-muted p-1">
						{#each typeOptions as opt}
							<button class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-all {editPoolType === opt ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => (editPoolType = opt)}>
								{typeLabel(opt)}
							</button>
						{/each}
					</div>
				</div>
				<div class="space-y-2">
					<Label class="text-sm font-medium">No Proxy (optional)</Label>
					<Input bind:value={editPoolNoProxy} placeholder="localhost,127.0.0.1" class="h-10 text-body-sm font-mono" />
				</div>
			</div>
			<Dialog.Footer>
				<Button variant="ghost" onclick={() => (showEditPool = false)}>Cancel</Button>
				<Button onclick={handleEditPool} disabled={editPoolLoading || !editPoolName.trim() || !editPoolUrl.trim()}>
					{editPoolLoading ? 'Saving...' : 'Save'}
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
