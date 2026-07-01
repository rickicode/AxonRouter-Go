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
  import { Card, CardContent } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import {
    RefreshCw, Gauge, AlertCircle, Clock, Search, ChevronLeft, ChevronRight,
    X, Infinity, AlertTriangle, CheckCircle2, HelpCircle, Zap, User
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
    if (pct > 50) return 'bg-emerald-500';
    if (pct > 20) return 'bg-amber-500';
    return 'bg-rose-500';
  }

  function quotaTextClr(pct: number): string {
    if (pct > 50) return 'text-emerald-400';
    if (pct > 20) return 'text-amber-400';
    return 'text-rose-400';
  }

  function statusInfo(status: string) {
    switch (status) {
      case 'ok': return { label: 'OK', cls: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400', icon: CheckCircle2 };
      case 'exhausted': return { label: 'Exhausted', cls: 'border-rose-500/30 bg-rose-500/10 text-rose-400', icon: AlertTriangle };
      case 'unlimited': return { label: 'Unlimited', cls: 'border-violet-500/30 bg-violet-500/10 text-violet-400', icon: Infinity };
      case 'error': return { label: 'Error', cls: 'border-rose-500/30 bg-rose-500/10 text-rose-400', icon: AlertCircle };
      default: return { label: 'No Data', cls: 'border-muted bg-muted/10 text-muted-foreground', icon: HelpCircle };
    }
  }

  function modelDisplayName(name: string): string {
    const map: Record<string, string> = {
      'gemini-2.5-pro': 'Gemini 3.1 Pro',
      'gemini-2.5-flash': 'Gemini 3.5 Flash',
      'gemini-3-flash': 'Gemini 3.5 Flash',
      'claude-sonnet-4-6': 'Claude Sonnet',
    };
    return map[name] || name;
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
    if (p.includes('free')) return { cls: 'border-muted text-muted-foreground', icon: User };
    if (p.includes('plus') || p.includes('pro')) return { cls: 'border-emerald-500/30 text-emerald-400', icon: Zap };
    if (p.includes('ultra') || p.includes('premium')) return { cls: 'border-violet-500/30 text-violet-400', icon: Zap };
    return { cls: 'border-muted text-muted-foreground', icon: Zap };
  }

  let providerOptions = $derived(() => {
    const opts = [{ value: '', label: 'All Providers' }];
    for (const p of $quotaSummary) opts.push({ value: p.provider_id, label: p.display_name });
    return opts;
  });

  const hasActiveFilters = $derived(filterProvider || filterStatus || searchQuery);
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div class="space-y-1">
      <h1 class="text-display-lg">Quota Tracker</h1>
      <p class="text-body-sm text-muted-foreground">Cached upstream quota · updated by background scheduler every 30 min</p>
    </div>
    <div class="flex items-center gap-2">
      {#if hasActiveFilters}
        <Button variant="outline" size="sm" onclick={clearFilters} class="gap-1.5 text-body-sm rounded-sm cursor-pointer">
          <X class="size-3.5" /> Clear
        </Button>
      {/if}
      <Button
        variant="outline"
        size="sm"
        onclick={handleRefreshAll}
        disabled={$quotaLoading || $quotaItems.length === 0}
        class="gap-1.5 text-body-sm rounded-sm cursor-pointer"
      >
        <RefreshCw class="size-3.5" /> Refresh Page
      </Button>
    </div>
  </div>

  <!-- Provider summary cards -->
  {#if $quotaSummary.length > 0}
    <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {#each $quotaSummary as ps}
        {@const exhausted = ps.statuses['exhausted'] || 0}
        {@const errors = ps.statuses['error'] || 0}
        {@const ok = (ps.statuses['ok'] || 0) + (ps.statuses['unlimited'] || 0)}
        <button
          onclick={() => onProviderChange(filterProvider === ps.provider_id ? '' : ps.provider_id)}
          class="rounded-lg bg-card p-4 text-left transition-all cursor-pointer shadow-card hover:shadow-elevated
            {filterProvider === ps.provider_id ? 'ring-1 ring-primary/40' : ''}"
        >
          <p class="text-caption text-muted-foreground">{ps.display_name}</p>
          <p class="mt-1 text-display-md">{ps.total}</p>
          <div class="mt-1 flex items-center gap-2 text-caption-mono">
            <span class="text-emerald-400">{ok} ready</span>
            {#if exhausted > 0}<span class="text-rose-400">{exhausted} exhausted</span>{/if}
            {#if errors > 0}<span class="text-amber-400">{errors} error</span>{/if}
          </div>
        </button>
      {/each}
    </div>
  {/if}

  <!-- Filters -->
  <section class="rounded-xl bg-card p-4 shadow-card md:p-5">
    <div class="flex flex-col gap-3 lg:flex-row lg:items-center">
      <div class="relative flex-1 lg:max-w-md">
        <Search class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
        <Input
          type="text"
          bind:value={searchQuery}
          oninput={onSearchInput}
          placeholder="Search connections…"
          class="h-10 pl-9 text-body-sm"
        />
      </div>
      <div class="flex items-center gap-2">
        <select
          class="h-10 rounded-md border border-input bg-background px-3 text-body-sm text-foreground cursor-pointer shrink-0"
          value={filterProvider}
          onchange={(e) => onProviderChange((e.target as HTMLSelectElement).value)}
        >
          {#each providerOptions() as opt}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
        <select
          class="h-10 rounded-md border border-input bg-background px-3 text-body-sm text-foreground cursor-pointer shrink-0"
          value={filterStatus}
          onchange={(e) => onStatusChange((e.target as HTMLSelectElement).value)}
        >
          {#each statusOptions as opt}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
      </div>
    </div>
  </section>

  <!-- Content -->
  {#if $quotaLoading && $quotaItems.length === 0}
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {#each [1, 2, 3, 4, 5, 6] as _}
        <div class="h-[200px] rounded-xl bg-card shadow-card animate-pulse"></div>
      {/each}
    </div>
  {:else if $quotaError && $quotaItems.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12 gap-3">
        <AlertCircle class="size-6 text-rose-400" />
        <p class="text-body-sm text-muted-foreground">{$quotaError}</p>
        <Button variant="outline" onclick={() => applyFilters()} class="text-body-sm rounded-sm cursor-pointer">Retry</Button>
      </CardContent>
    </Card>
  {:else if $quotaItems.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12 gap-3">
        <Gauge class="size-6 text-muted-foreground" />
        <p class="text-body-sm text-muted-foreground">
          {hasActiveFilters ? 'No connections match your filters.' : 'No quota data yet. The scheduler will populate this on its next run.'}
        </p>
        {#if hasActiveFilters}
          <Button variant="outline" onclick={clearFilters} class="text-body-sm rounded-sm cursor-pointer">Clear Filters</Button>
        {/if}
      </CardContent>
    </Card>
  {:else}
    <!-- Card grid -->
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {#each $quotaItems as item (item.id)}
        {@const st = statusInfo(item.status)}
        {@const stIcon = st.icon}
        {@const refreshing = isRefreshing(item.connection_id)}
        {@const planBadge = item.plan ? getPlanBadge(item.plan) : null}
        <div class="rounded-xl bg-card shadow-card transition-all hover:shadow-elevated {refreshing ? 'ring-1 ring-primary/30' : ''}">
          <!-- Card header -->
          <div class="flex items-center justify-between px-4 pt-4 pb-2">
            <div class="min-w-0 flex items-center gap-2">
              <span class="size-2.5 rounded-full shrink-0" style="background-color: {item.color}"></span>
              <span class="text-body-sm-strong truncate">{item.connection_name}</span>
            </div>
            <button
              onclick={() => handleRefreshOne(item.connection_id)}
              disabled={refreshing}
              class="size-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors cursor-pointer disabled:opacity-40"
              title="Refresh"
            >
              <RefreshCw class="size-3.5 {refreshing ? 'animate-spin' : ''}" />
            </button>
          </div>

          <!-- Meta row -->
          <div class="flex items-center gap-2 px-4 pb-3">
            <span class="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-caption {st.cls}">
              <stIcon class="size-3"></stIcon>
              {st.label}
            </span>
            {#if planBadge}
              <span class="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-caption {planBadge.cls}">
                <planBadge.icon class="size-3"></planBadge.icon>
                {item.plan}
              </span>
            {/if}
            <span class="text-caption text-muted-foreground">{item.display_name}</span>
          </div>

          <!-- Quota bars -->
          <div class="px-4 pb-3 space-y-2.5">
            {#if item.error}
              <div class="flex items-start gap-2 rounded-md bg-rose-500/5 border border-rose-500/10 px-3 py-2">
                <AlertCircle class="size-3.5 text-rose-400 shrink-0 mt-0.5" />
                <p class="text-caption text-rose-400/80 leading-snug">{item.error}</p>
              </div>
            {:else if item.quotas.length === 0}
              <p class="text-caption text-muted-foreground">No quota data.</p>
            {:else}
              {#each item.quotas as qi}
                <div class="space-y-1">
                  <div class="flex items-center justify-between">
                    <span class="text-caption text-muted-foreground truncate max-w-[60%]">{modelDisplayName(qi.name)}</span>
                    <div class="flex items-center gap-2">
                      {#if qi.unlimited}
                        <span class="text-caption text-violet-400 font-medium">∞ unlimited</span>
                      {:else}
                        <span class="text-caption-mono font-semibold tabular-nums {quotaTextClr(qi.remaining_pct)}">
                          {qi.remaining_pct.toFixed(0)}%
                        </span>
                      {/if}
                      {#if qi.reset_at}
                        <span class="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground">
                          <Clock class="size-2.5" />{formatResetTime(qi.reset_at)}
                        </span>
                      {/if}
                    </div>
                  </div>
                  {#if !qi.unlimited}
                    <div class="h-1.5 rounded-full bg-muted overflow-hidden">
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
          <div class="px-4 py-2.5 border-t border-border">
            <span class="text-caption text-muted-foreground">Updated {formatFetched(item.fetched_at)}</span>
          </div>
        </div>
      {/each}
    </div>

    <!-- Pagination -->
    {#if $quotaTotalPages > 1}
      <div class="flex items-center justify-between">
        <p class="text-caption text-muted-foreground">
          Showing {($quotaPage - 1) * perPage + 1}–{Math.min($quotaPage * perPage, $quotaTotal)} of {$quotaTotal}
        </p>
        <div class="flex items-center gap-1.5">
          <Button variant="outline" size="icon" class="size-8 rounded-sm cursor-pointer" disabled={$quotaPage <= 1} onclick={() => goToPage($quotaPage - 1)}>
            <ChevronLeft class="size-4" />
          </Button>
          <span class="text-caption-mono px-2">{$quotaPage} / {$quotaTotalPages}</span>
          <Button variant="outline" size="icon" class="size-8 rounded-sm cursor-pointer" disabled={$quotaPage >= $quotaTotalPages} onclick={() => goToPage($quotaPage + 1)}>
            <ChevronRight class="size-4" />
          </Button>
        </div>
      </div>
    {/if}
  {/if}
</div>
