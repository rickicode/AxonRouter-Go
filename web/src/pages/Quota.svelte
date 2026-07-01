<script lang="ts">
  import { onMount } from 'svelte';
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
  import type { QuotaCacheEntry } from '$lib/api';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import {
    RefreshCw, Gauge, AlertCircle, Clock, Search, ChevronLeft, ChevronRight,
    X, Infinity, AlertTriangle, CheckCircle2, HelpCircle, Zap, User, Building2
  } from '@lucide/svelte';

  let searchQuery = $state('');
  let filterProvider = $state('');
  let filterStatus = $state('');
  let searchTimeout: ReturnType<typeof setTimeout> | undefined;
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
    if (page >= 1 && page <= $quotaTotalPages) applyFilters(page);
  }

  function isRefreshing(connId: string) { return refreshingIds.has(connId); }
  function setRefreshing(connId: string, val: boolean) {
    refreshingIds = new Set(refreshingIds);
    if (val) refreshingIds.add(connId); else refreshingIds.delete(connId);
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
      await Promise.allSettled(batch.map(c => {
        setRefreshing(c.connection_id, true);
        return refreshConnectionQuota(c.connection_id).finally(() => setRefreshing(c.connection_id, false));
      }));
    }
    loadQuotaSummary();
  }

  function quotaBarColor(pct: number): string {
    if (pct > 50) return 'bg-[#4ade80]';
    if (pct > 20) return 'bg-[#fbbf24]';
    return 'bg-[#f87171]';
  }

  function quotaTextClr(pct: number): string {
    if (pct > 50) return 'text-[#4ade80]';
    if (pct > 20) return 'text-[#fbbf24]';
    return 'text-[#f87171]';
  }

  function statusInfo(status: string) {
    switch (status) {
      case 'ok': return { label: 'OK', bg: 'bg-[#4ade80]/10', border: 'border-[#4ade80]/20', text: 'text-[#4ade80]', icon: CheckCircle2 };
      case 'exhausted': return { label: 'Exhausted', bg: 'bg-[#f87171]/10', border: 'border-[#f87171]/20', text: 'text-[#f87171]', icon: AlertTriangle };
      case 'unlimited': return { label: 'Unlimited', bg: 'bg-[#a78bfa]/10', border: 'border-[#a78bfa]/20', text: 'text-[#a78bfa]', icon: Infinity };
      case 'error': return { label: 'Error', bg: 'bg-[#f87171]/10', border: 'border-[#f87171]/20', text: 'text-[#f87171]', icon: AlertCircle };
      default: return { label: 'No Data', bg: 'bg-[#71717a]/10', border: 'border-[#71717a]/20', text: 'text-[#71717a]', icon: HelpCircle };
    }
  }

  function formatResetTime(iso?: string): string {
    if (!iso) return '';
    try {
      const diffMs = new Date(iso).getTime() - Date.now();
      if (diffMs <= 0) return 'soon';
      const h = Math.floor(diffMs / 3_600_000);
      const m = Math.floor((diffMs % 3_600_000) / 60_000);
      if (h > 24) return `${Math.floor(h / 24)}d`;
      if (h > 0) return `${h}h ${m}m`;
      return `${m}m`;
    } catch { return ''; }
  }

  function formatFetched(ms: number): string {
    if (!ms) return '';
    const diffMs = Date.now() - ms;
    if (diffMs < 60_000) return 'just now';
    if (diffMs < 3_600_000) return `${Math.floor(diffMs / 60_000)}m ago`;
    if (diffMs < 86_400_000) return `${Math.floor(diffMs / 3_600_000)}h ago`;
    return new Date(ms).toLocaleDateString();
  }

  function getPlanBadge(plan: string) {
    const p = plan.toLowerCase();
    if (p.includes('free')) return { cls: 'border-[#71717a]/30 text-[#71717a]', icon: User };
    if (p.includes('plus') || p.includes('pro')) return { cls: 'border-[#4ade80]/30 text-[#4ade80]', icon: Zap };
    if (p.includes('ultra') || p.includes('premium')) return { cls: 'border-[#a78bfa]/30 text-[#a78bfa]', icon: Zap };
    return { cls: 'border-[#71717a]/30 text-[#71717a]', icon: Zap };
  }

  let providerOptions = $derived(() => {
    const opts = [{ value: '', label: 'All Providers' }];
    for (const p of $quotaSummary) opts.push({ value: p.provider_id, label: p.display_name });
    return opts;
  });

  const hasActiveFilters = $derived(filterProvider || filterStatus || searchQuery);
</script>

<div class="flex flex-1 flex-col gap-6 p-6 max-w-[1400px]">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-[24px] font-semibold tracking-[-0.96px] text-[#e4e4e7] leading-[32px]">Quota Tracker</h1>
      <p class="text-[13px] text-[#71717a] mt-0.5">Cached upstream quota · updated by background scheduler every 30 min</p>
    </div>
    <div class="flex items-center gap-2">
      {#if hasActiveFilters}
        <button
          onclick={clearFilters}
          class="inline-flex items-center gap-1.5 h-[32px] px-3 rounded-[6px] text-[13px] font-medium text-[#a1a1aa] bg-[#18181b] border border-[#27272a] hover:text-[#e4e4e7] hover:border-[#3f3f46] transition-colors cursor-pointer"
        >
          <X class="size-3.5" /> Clear
        </button>
      {/if}
      <button
        onclick={handleRefreshAll}
        disabled={$quotaLoading || $quotaItems.length === 0}
        class="inline-flex items-center gap-1.5 h-[32px] px-3 rounded-[6px] text-[13px] font-medium text-[#e4e4e7] bg-[#18181b] border border-[#27272a] hover:border-[#3f3f46] transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
      >
        <RefreshCw class="size-3.5" /> Refresh Page
      </button>
    </div>
  </div>

  <!-- Provider summary pills -->
  {#if $quotaSummary.length > 0}
    <div class="flex flex-wrap gap-2">
      {#each $quotaSummary as ps}
        {@const exhausted = ps.statuses['exhausted'] || 0}
        {@const errors = ps.statuses['error'] || 0}
        {@const ok = (ps.statuses['ok'] || 0) + (ps.statuses['unlimited'] || 0)}
        <button
          onclick={() => onProviderChange(filterProvider === ps.provider_id ? '' : ps.provider_id)}
          class="inline-flex items-center gap-2.5 h-[36px] px-3.5 rounded-[6px] text-[13px] transition-all cursor-pointer
            {filterProvider === ps.provider_id
              ? 'bg-[#ec4899]/10 border border-[#ec4899]/30 text-[#ec4899]'
              : 'bg-[#18181b] border border-[#27272a] text-[#a1a1aa] hover:border-[#3f3f46] hover:text-[#e4e4e7]'}"
        >
          <span class="font-medium">{ps.display_name}</span>
          <span class="inline-flex items-center gap-1.5 text-[12px] font-mono">
            <span class="text-[#4ade80]">{ok}</span>
            {#if exhausted > 0}<span class="text-[#f87171]">{exhausted}</span>{/if}
            {#if errors > 0}<span class="text-[#fbbf24]">{errors}</span>{/if}
            <span class="text-[#71717a]">/{ps.total}</span>
          </span>
        </button>
      {/each}
    </div>
  {/if}

  <!-- Filters -->
  <div class="flex flex-wrap items-center gap-2">
    <div class="relative flex-1 min-w-[200px] max-w-sm">
      <Search class="absolute left-3 top-1/2 -translate-y-1/2 size-3.5 text-[#71717a]" />
      <input
        type="text"
        bind:value={searchQuery}
        oninput={onSearchInput}
        placeholder="Search connections…"
        class="w-full h-[36px] pl-9 pr-3 rounded-[6px] bg-[#18181b] border border-[#27272a] text-[13px] text-[#e4e4e7] placeholder:text-[#71717a] focus:outline-none focus:border-[#ec4899]/50 transition-colors"
      />
    </div>
    <select
      class="h-[36px] px-3 rounded-[6px] bg-[#18181b] border border-[#27272a] text-[13px] text-[#e4e4e7] cursor-pointer focus:outline-none focus:border-[#ec4899]/50 transition-colors"
      value={filterProvider}
      onchange={(e) => onProviderChange((e.target as HTMLSelectElement).value)}
    >
      {#each providerOptions() as opt}
        <option value={opt.value}>{opt.label}</option>
      {/each}
    </select>
    <select
      class="h-[36px] px-3 rounded-[6px] bg-[#18181b] border border-[#27272a] text-[13px] text-[#e4e4e7] cursor-pointer focus:outline-none focus:border-[#ec4899]/50 transition-colors"
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
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each [1, 2, 3, 4, 5, 6] as _}
        <div class="h-[180px] rounded-[8px] bg-[#18181b] border border-[#27272a] animate-pulse"></div>
      {/each}
    </div>
  {:else if $quotaError && $quotaItems.length === 0}
    <div class="flex flex-col items-center justify-center py-16 gap-3 rounded-[8px] bg-[#18181b] border border-[#27272a]">
      <AlertCircle class="size-6 text-[#f87171]" />
      <p class="text-[13px] text-[#a1a1aa]">{$quotaError}</p>
      <button onclick={() => applyFilters()} class="h-[32px] px-3 rounded-[6px] text-[13px] font-medium text-[#e4e4e7] bg-[#18181b] border border-[#27272a] hover:border-[#3f3f46] cursor-pointer">Retry</button>
    </div>
  {:else if $quotaItems.length === 0}
    <div class="flex flex-col items-center justify-center py-16 gap-3 rounded-[8px] bg-[#18181b] border border-[#27272a]">
      <Gauge class="size-6 text-[#71717a]" />
      <p class="text-[13px] text-[#a1a1aa]">
        {hasActiveFilters ? 'No connections match your filters.' : 'No quota data yet. The scheduler will populate this on its next run.'}
      </p>
      {#if hasActiveFilters}
        <button onclick={clearFilters} class="h-[32px] px-3 rounded-[6px] text-[13px] font-medium text-[#e4e4e7] bg-[#18181b] border border-[#27272a] hover:border-[#3f3f46] cursor-pointer">Clear Filters</button>
      {/if}
    </div>
  {:else}
    <!-- Card grid -->
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each $quotaItems as item (item.id)}
        {@const st = statusInfo(item.status)}
        {@const stIcon = st.icon}
        {@const refreshing = isRefreshing(item.connection_id)}
        {@const planBadge = item.plan ? getPlanBadge(item.plan) : null}
        <div class="group relative flex flex-col rounded-[8px] bg-[#18181b] border border-[#27272a] hover:border-[#3f3f46] transition-colors {refreshing ? 'border-[#ec4899]/30' : ''}" style="box-shadow: inset 0 0 0 1px rgba(255,255,255,0.03);">
          <!-- Card header -->
          <div class="flex items-center justify-between px-4 pt-3.5 pb-2">
            <div class="min-w-0 flex items-center gap-2">
              <span class="size-2 rounded-full shrink-0" style="background-color: {item.color}"></span>
              <span class="text-[14px] font-medium text-[#e4e4e7] truncate tracking-[-0.28px]">{item.connection_name}</span>
            </div>
            <button
              onclick={() => handleRefreshOne(item.connection_id)}
              disabled={refreshing}
              class="size-6 flex items-center justify-center rounded-[4px] text-[#71717a] hover:text-[#e4e4e7] hover:bg-[#27272a] transition-colors cursor-pointer disabled:opacity-40"
              title="Refresh"
            >
              <RefreshCw class="size-3 {refreshing ? 'animate-spin' : ''}" />
            </button>
          </div>

          <!-- Meta row -->
          <div class="flex items-center gap-2 px-4 pb-2">
            <span class="inline-flex items-center gap-1 h-[20px] px-1.5 rounded-full border {st.bg} {st.border} {st.text} text-[11px] font-medium">
              <stIcon class="size-2.5"></stIcon>
              {st.label}
            </span>
            {#if planBadge}
              <span class="inline-flex items-center gap-1 h-[20px] px-1.5 rounded-full border {planBadge.cls} text-[11px] font-medium border-current/20">
                <planBadge.icon class="size-2.5"></planBadge.icon>
                {item.plan}
              </span>
            {/if}
            <span class="text-[11px] text-[#71717a] font-mono">{item.display_name}</span>
          </div>

          <!-- Quota bars -->
          <div class="flex-1 px-4 pb-2 space-y-2">
            {#if item.error}
              <div class="flex items-start gap-2 rounded-[4px] bg-[#f87171]/5 border border-[#f87171]/10 px-2.5 py-2">
                <AlertCircle class="size-3 text-[#f87171] shrink-0 mt-0.5" />
                <p class="text-[12px] text-[#f87171]/80 leading-snug">{item.error}</p>
              </div>
            {:else if item.quotas.length === 0}
              <p class="text-[12px] text-[#71717a]">No quota data.</p>
            {:else}
              {#each item.quotas as qi}
                <div class="space-y-1">
                  <div class="flex items-center justify-between">
                    <span class="text-[12px] text-[#a1a1aa] truncate max-w-[60%]">{qi.name}</span>
                    <div class="flex items-center gap-2">
                      {#if qi.unlimited}
                        <span class="text-[12px] text-[#a78bfa] font-medium">∞ unlimited</span>
                      {:else}
                        <span class="text-[12px] font-semibold font-mono tabular-nums {quotaTextClr(qi.remaining_pct)}">
                          {qi.remaining_pct.toFixed(0)}%
                        </span>
                      {/if}
                      {#if qi.reset_at}
                        <span class="inline-flex items-center gap-0.5 text-[10px] text-[#71717a]">
                          <Clock class="size-2.5" />{formatResetTime(qi.reset_at)}
                        </span>
                      {/if}
                    </div>
                  </div>
                  {#if !qi.unlimited}
                    <div class="h-[3px] rounded-full bg-[#27272a] overflow-hidden">
                      <div
                        class="h-full rounded-full transition-all duration-500 {quotaBarColor(qi.remaining_pct)}"
                        style="width: {qi.remaining_pct}%"
                      ></div>
                    </div>
                  {/if}
                </div>
              {/each}
            {/if}
          </div>

          <!-- Footer -->
          <div class="px-4 py-2.5 border-t border-[#27272a] flex items-center justify-between">
            <span class="text-[11px] text-[#71717a]">Updated {formatFetched(item.fetched_at)}</span>
          </div>
        </div>
      {/each}
    </div>

    <!-- Pagination -->
    {#if $quotaTotalPages > 1}
      <div class="flex items-center justify-between pt-1">
        <p class="text-[12px] text-[#71717a]">
          Showing {($quotaPage - 1) * perPage + 1}–{Math.min($quotaPage * perPage, $quotaTotal)} of {$quotaTotal}
        </p>
        <div class="flex items-center gap-1.5">
          <button
            disabled={$quotaPage <= 1}
            onclick={() => goToPage($quotaPage - 1)}
            class="size-7 flex items-center justify-center rounded-[4px] border border-[#27272a] text-[#a1a1aa] hover:text-[#e4e4e7] hover:border-[#3f3f46] transition-colors cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <ChevronLeft class="size-3.5" />
          </button>
          <span class="text-[12px] text-[#71717a] font-mono px-2">{$quotaPage} / {$quotaTotalPages}</span>
          <button
            disabled={$quotaPage >= $quotaTotalPages}
            onclick={() => goToPage($quotaPage + 1)}
            class="size-7 flex items-center justify-center rounded-[4px] border border-[#27272a] text-[#a1a1aa] hover:text-[#e4e4e7] hover:border-[#3f3f46] transition-colors cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <ChevronRight class="size-3.5" />
          </button>
        </div>
      </div>
    {/if}
  {/if}
</div>
