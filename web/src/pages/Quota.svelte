<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    quotaItems,
    quotaTotal,
    quotaPage,
    quotaTotalPages,
    quotaLoading,
    quotaError,
    quotaSummary,
    loadQuota,
    loadQuotaSummary,
    refreshConnectionQuota,
  } from '$lib/stores';
  import type { QuotaCacheEntry, QuotaProviderSummary } from '$lib/api';
  import { quotaApi } from '$lib/api';
  import { Card, CardContent } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import {
    RefreshCw, Gauge, AlertCircle, Clock, Search, ChevronLeft, ChevronRight,
    Filter, X, Zap, Infinity, AlertTriangle, CheckCircle2, HelpCircle
  } from '@lucide/svelte';

  // Filter state
  let searchQuery = $state('');
  let filterProvider = $state('');
  let filterStatus = $state('');
  let searchTimeout: ReturnType<typeof setTimeout> | undefined;

  // Per-row refresh state
  let refreshingIds = $state<Set<string>>(new Set());

  const perPage = 50;

  const statusOptions = [
    { value: '', label: 'All Status' },
    { value: 'ok', label: 'OK' },
    { value: 'exhausted', label: 'Exhausted' },
    { value: 'unlimited', label: 'Unlimited' },
    { value: 'error', label: 'Error' },
    { value: 'no_data', label: 'No Data' },
  ];

  onMount(() => {
    document.title = 'Quota — AxonRouter';
    loadQuota({ page: 1, per_page: perPage });
    loadQuotaSummary();
  });

  function applyFilters(page = 1) {
    loadQuota({
      provider: filterProvider || undefined,
      search: searchQuery || undefined,
      status: filterStatus || undefined,
      page,
      per_page: perPage,
    });
  }

  function onSearchInput() {
    clearTimeout(searchTimeout);
    searchTimeout = setTimeout(() => applyFilters(), 300);
  }

  function onProviderChange(val: string) {
    filterProvider = val;
    applyFilters();
  }

  function onStatusChange(val: string) {
    filterStatus = val;
    applyFilters();
  }

  function clearFilters() {
    searchQuery = '';
    filterProvider = '';
    filterStatus = '';
    applyFilters();
  }

  function goToPage(page: number) {
    if (page >= 1 && page <= $quotaTotalPages) {
      applyFilters(page);
    }
  }

  function isRefreshing(connId: string): boolean {
    return refreshingIds.has(connId);
  }

  function setRefreshing(connId: string, val: boolean) {
    refreshingIds = new Set(refreshingIds);
    if (val) refreshingIds.add(connId);
    else refreshingIds.delete(connId);
  }

  async function handleRefreshOne(connId: string) {
    setRefreshing(connId, true);
    await refreshConnectionQuota(connId);
    setRefreshing(connId, false);
    loadQuotaSummary();
  }

  async function handleRefreshAll() {
    const items = $quotaItems;
    for (let i = 0; i < items.length; i += 3) {
      const batch = items.slice(i, i + 3);
      await Promise.allSettled(
        batch.map(c => {
          setRefreshing(c.connection_id, true);
          return refreshConnectionQuota(c.connection_id).finally(() =>
            setRefreshing(c.connection_id, false)
          );
        })
      );
    }
    loadQuotaSummary();
  }

  function quotaTextColor(pct: number): string {
    if (pct > 50) return 'text-emerald-400';
    if (pct > 20) return 'text-amber-400';
    return 'text-rose-400';
  }

  function quotaBarColor(pct: number): string {
    if (pct > 50) return 'bg-emerald-500';
    if (pct > 20) return 'bg-amber-500';
    return 'bg-rose-500';
  }

  function statusBadge(status: string) {
    switch (status) {
      case 'ok': return { label: 'OK', cls: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-300', icon: CheckCircle2 };
      case 'exhausted': return { label: 'Exhausted', cls: 'border-rose-500/40 bg-rose-500/10 text-rose-300', icon: AlertTriangle };
      case 'unlimited': return { label: 'Unlimited', cls: 'border-violet-500/40 bg-violet-500/10 text-violet-300', icon: Infinity };
      case 'error': return { label: 'Error', cls: 'border-rose-500/40 bg-rose-500/10 text-rose-300', icon: AlertCircle };
      case 'no_data': return { label: 'No Data', cls: 'border-zinc-500/40 bg-zinc-500/10 text-zinc-400', icon: HelpCircle };
      default: return { label: status, cls: 'border-zinc-500/40 bg-zinc-500/10 text-zinc-400', icon: HelpCircle };
    }
  }

  function formatTimestamp(ms: number): string {
    if (!ms) return '';
    const d = new Date(ms);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    if (diffMs < 60_000) return 'just now';
    if (diffMs < 3_600_000) return `${Math.floor(diffMs / 60_000)}m ago`;
    if (diffMs < 86_400_000) return `${Math.floor(diffMs / 3_600_000)}h ago`;
    return d.toLocaleDateString();
  }

  function formatResetTime(iso?: string): string {
    if (!iso) return '';
    try {
      const date = new Date(iso);
      const now = new Date();
      const diffMs = date.getTime() - now.getTime();
      if (diffMs <= 0) return 'soon';
      const hours = Math.floor(diffMs / 3_600_000);
      const mins = Math.floor((diffMs % 3_600_000) / 60_000);
      if (hours > 24) return `${Math.floor(hours / 24)}d`;
      if (hours > 0) return `${hours}h ${mins}m`;
      return `${mins}m`;
    } catch {
      return '';
    }
  }

  // Build a compact quota summary string per connection
  function quotaSummaryText(item: QuotaCacheEntry): string {
    if (item.error) return item.error;
    if (!item.quotas.length) return 'No data';
    const nonUnlimited = item.quotas.filter(q => !q.unlimited);
    if (nonUnlimited.length === 0) return 'All unlimited';
    const min = Math.min(...nonUnlimited.map(q => q.remaining_pct));
    return `${min.toFixed(0)}% min remaining`;
  }

  // Unique providers from summary
  let providerOptions = $derived(() => {
    const opts = [{ value: '', label: 'All Providers' }];
    for (const p of $quotaSummary) {
      opts.push({ value: p.provider_id, label: p.display_name });
    }
    return opts;
  });

  const hasActiveFilters = $derived(filterProvider || filterStatus || searchQuery);
</script>

<div class="flex flex-1 flex-col gap-4 p-4 max-w-[1400px]">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-lg font-semibold">Quota Tracker</h1>
      <p class="text-xs text-muted-foreground">Cached upstream quota data · updated by background scheduler</p>
    </div>
    <div class="flex items-center gap-2">
      {#if hasActiveFilters}
        <Button variant="ghost" size="sm" onclick={clearFilters} class="h-7 gap-1 text-xs cursor-pointer">
          <X class="size-3" /> Clear
        </Button>
      {/if}
      <Button
        variant="outline"
        size="sm"
        onclick={handleRefreshAll}
        disabled={$quotaLoading || $quotaItems.length === 0}
        class="h-7 gap-1.5 text-xs cursor-pointer"
      >
        <RefreshCw class="size-3" />
        Refresh Page
      </Button>
    </div>
  </div>

  <!-- Summary cards -->
  {#if $quotaSummary.length > 0}
    <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-2">
      {#each $quotaSummary as ps}
        {@const exhausted = ps.statuses['exhausted'] || 0}
        {@const errors = ps.statuses['error'] || 0}
        {@const ok = (ps.statuses['ok'] || 0) + (ps.statuses['unlimited'] || 0)}
        <button
          class="flex flex-col gap-1 rounded-lg border border-zinc-800 bg-zinc-900/50 px-3 py-2 text-left transition-colors hover:bg-zinc-800/50 cursor-pointer {filterProvider === ps.provider_id ? 'ring-1 ring-primary/50 border-primary/30' : ''}"
          onclick={() => onProviderChange(filterProvider === ps.provider_id ? '' : ps.provider_id)}
        >
          <span class="text-[11px] font-medium text-zinc-300 truncate">{ps.display_name}</span>
          <div class="flex items-center gap-2 text-[10px]">
            <span class="text-emerald-400">{ok}</span>
            {#if exhausted > 0}
              <span class="text-rose-400">{exhausted}</span>
            {/if}
            {#if errors > 0}
              <span class="text-amber-400">{errors}</span>
            {/if}
            <span class="text-zinc-500">/ {ps.total}</span>
          </div>
        </button>
      {/each}
    </div>
  {/if}

  <!-- Filters -->
  <div class="flex flex-wrap items-center gap-2">
    <div class="relative flex-1 min-w-[200px] max-w-sm">
      <Search class="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
      <Input
        bind:value={searchQuery}
        oninput={onSearchInput}
        placeholder="Search connections…"
        class="h-8 pl-8 text-xs"
      />
    </div>
    <select
      class="h-8 rounded-md border border-input bg-background px-2 text-xs text-foreground cursor-pointer"
      value={filterProvider}
      onchange={(e) => onProviderChange((e.target as HTMLSelectElement).value)}
    >
      {#each providerOptions() as opt}
        <option value={opt.value}>{opt.label}</option>
      {/each}
    </select>
    <select
      class="h-8 rounded-md border border-input bg-background px-2 text-xs text-foreground cursor-pointer"
      value={filterStatus}
      onchange={(e) => onStatusChange((e.target as HTMLSelectElement).value)}
    >
      {#each statusOptions as opt}
        <option value={opt.value}>{opt.label}</option>
      {/each}
    </select>
  </div>

  <!-- Content -->
  {#if $quotaLoading && $quotaItems.length === 0}
    <div class="flex flex-col gap-2">
      {#each [1, 2, 3, 4, 5] as _}
        <div class="h-12 rounded-lg bg-zinc-900 animate-pulse"></div>
      {/each}
    </div>
  {:else if $quotaError && $quotaItems.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-10 gap-2">
        <AlertCircle class="size-6 text-rose-400" />
        <p class="text-xs text-muted-foreground">{$quotaError}</p>
        <Button variant="outline" size="sm" onclick={() => applyFilters()} class="h-7 text-xs cursor-pointer">Retry</Button>
      </CardContent>
    </Card>
  {:else if $quotaItems.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-10 gap-2">
        <Gauge class="size-6 text-muted-foreground" />
        <p class="text-xs text-muted-foreground">
          {hasActiveFilters ? 'No connections match your filters.' : 'No quota data yet. The scheduler will populate this on its next run.'}
        </p>
        {#if hasActiveFilters}
          <Button variant="outline" size="sm" onclick={clearFilters} class="h-7 text-xs cursor-pointer">Clear Filters</Button>
        {/if}
      </CardContent>
    </Card>
  {:else}
    <!-- Table -->
    <div class="rounded-lg border border-zinc-800 overflow-hidden">
      <table class="w-full text-xs">
        <thead>
          <tr class="border-b border-zinc-800 bg-zinc-900/80">
            <th class="text-left px-3 py-2 font-medium text-zinc-400">Connection</th>
            <th class="text-left px-3 py-2 font-medium text-zinc-400">Provider</th>
            <th class="text-left px-3 py-2 font-medium text-zinc-400">Plan</th>
            <th class="text-left px-3 py-2 font-medium text-zinc-400">Status</th>
            <th class="text-left px-3 py-2 font-medium text-zinc-400">Quota</th>
            <th class="text-left px-3 py-2 font-medium text-zinc-400">Updated</th>
            <th class="text-right px-3 py-2 font-medium text-zinc-400 w-10"></th>
          </tr>
        </thead>
        <tbody>
          {#each $quotaItems as item (item.id)}
            {@const st = statusBadge(item.status)}
            {@const stIcon = st.icon}
            {@const refreshing = isRefreshing(item.connection_id)}
            <tr class="border-b border-zinc-800/50 hover:bg-zinc-900/50 transition-colors {refreshing ? 'bg-primary/5' : ''}">
              <!-- Connection name -->
              <td class="px-3 py-2">
                <span class="font-medium text-zinc-200 truncate max-w-[200px] block" title={item.connection_name}>
                  {item.connection_name}
                </span>
              </td>
              <!-- Provider -->
              <td class="px-3 py-2">
                <div class="flex items-center gap-1.5">
                  <span class="size-2 rounded-full shrink-0" style="background-color: {item.color}"></span>
                  <span class="text-zinc-300">{item.display_name}</span>
                </div>
              </td>
              <!-- Plan -->
              <td class="px-3 py-2">
                {#if item.plan}
                  <span class="text-zinc-400">{item.plan}</span>
                {:else}
                  <span class="text-zinc-600">—</span>
                {/if}
              </td>
              <!-- Status -->
              <td class="px-3 py-2">
                <span class="inline-flex items-center gap-1 rounded border px-1.5 py-px {st.cls}">
                  <stIcon class="size-2.5"></stIcon>
                  <span class="font-semibold">{st.label}</span>
                </span>
              </td>
              <!-- Quota summary -->
              <td class="px-3 py-2">
                {#if item.error}
                  <span class="text-rose-400 truncate max-w-[250px] block" title={item.error}>{item.error}</span>
                {:else if item.quotas.length === 0}
                  <span class="text-zinc-600">No data</span>
                {:else}
                  <div class="flex flex-col gap-0.5">
                    {#each item.quotas.slice(0, 3) as qi}
                      <div class="flex items-center gap-2">
                        <span class="text-zinc-500 w-16 truncate" title={qi.name}>{qi.name}</span>
                        {#if qi.unlimited}
                          <span class="text-emerald-400 text-[10px]">∞</span>
                        {:else}
                          <div class="flex-1 max-w-[80px]">
                            <div class="h-1 rounded-full bg-zinc-800 overflow-hidden">
                              <div
                                class="h-full rounded-full transition-all duration-300 {quotaBarColor(qi.remaining_pct)}"
                                style="width: {qi.remaining_pct}%"
                              ></div>
                            </div>
                          </div>
                          <span class="font-mono font-bold tabular-nums w-10 text-right {quotaTextColor(qi.remaining_pct)}">
                            {qi.remaining_pct.toFixed(0)}%
                          </span>
                        {/if}
                        {#if qi.reset_at}
                          <span class="text-zinc-600 text-[9px]">
                            <Clock class="size-2 inline" /> {formatResetTime(qi.reset_at)}
                          </span>
                        {/if}
                      </div>
                    {/each}
                    {#if item.quotas.length > 3}
                      <span class="text-zinc-600 text-[10px]">+{item.quotas.length - 3} more</span>
                    {/if}
                  </div>
                {/if}
              </td>
              <!-- Updated -->
              <td class="px-3 py-2 text-zinc-500">
                {formatTimestamp(item.fetched_at)}
              </td>
              <!-- Actions -->
              <td class="px-3 py-2 text-right">
                <Button
                  variant="ghost"
                  size="icon"
                  class="size-6 cursor-pointer text-muted-foreground hover:text-foreground"
                  onclick={() => handleRefreshOne(item.connection_id)}
                  disabled={refreshing}
                  title="Refresh this connection"
                >
                  <RefreshCw class="size-3 {refreshing ? 'animate-spin' : ''}" />
                </Button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <!-- Pagination -->
    {#if $quotaTotalPages > 1}
      <div class="flex items-center justify-between">
        <p class="text-xs text-muted-foreground">
          Showing {($quotaPage - 1) * perPage + 1}–{Math.min($quotaPage * perPage, $quotaTotal)} of {$quotaTotal}
        </p>
        <div class="flex items-center gap-1">
          <Button
            variant="outline"
            size="icon"
            class="size-7 cursor-pointer"
            disabled={$quotaPage <= 1}
            onclick={() => goToPage($quotaPage - 1)}
          >
            <ChevronLeft class="size-3.5" />
          </Button>
          <span class="text-xs text-zinc-400 px-2">
            {$quotaPage} / {$quotaTotalPages}
          </span>
          <Button
            variant="outline"
            size="icon"
            class="size-7 cursor-pointer"
            disabled={$quotaPage >= $quotaTotalPages}
            onclick={() => goToPage($quotaPage + 1)}
          >
            <ChevronRight class="size-3.5" />
          </Button>
        </div>
      </div>
    {/if}
  {/if}
</div>
