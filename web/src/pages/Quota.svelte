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
  quotaSavings,
  quotaNextReset,
  loadQuota,
  loadQuotaSummary,
  refreshConnectionQuota,
  formatCost,
} from '$lib/stores';
  import type { QuotaCacheEntry, QuotaItem } from '$lib/api';
  import { getTokenExpiry } from '$lib/utils';
  import { Card, CardContent } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import GaugeIcon from '@lucide/svelte/icons/gauge';
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
import ClockIcon from '@lucide/svelte/icons/clock';
import TimerIcon from '@lucide/svelte/icons/timer';
import DollarSignIcon from '@lucide/svelte/icons/dollar-sign';
import SearchIcon from '@lucide/svelte/icons/search';
  import XIcon from '@lucide/svelte/icons/x';
  import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
  import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
  import HelpCircleIcon from '@lucide/svelte/icons/help-circle';
  import ZapIcon from '@lucide/svelte/icons/zap';
  import UserIcon from '@lucide/svelte/icons/user';
  import Pagination from '$lib/components/Pagination.svelte';

  let searchQuery = $state('');
  let filterProvider = $state('');
  let filterStatus = $state('');
  let searchTimeout: ReturnType<typeof setTimeout> | undefined;
  let refreshingIds = $state<Set<string>>(new Set());

  let perPage = $state(50);

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
      case 'ok': return { label: 'Ready', cls: 'text-emerald-400', dot: 'bg-emerald-400' };
      case 'exhausted': return { label: 'Exhausted', cls: 'text-rose-400', dot: 'bg-rose-400' };
      case 'unlimited': return { label: 'Unlimited', cls: 'text-violet-400', dot: 'bg-violet-400' };
      case 'error': return { label: 'Error', cls: 'text-rose-400', dot: 'bg-rose-400' };
      default: return { label: 'No Data', cls: 'text-muted-foreground', dot: 'bg-muted-foreground' };
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

// For Antigravity, only show the three main model families on the Quota page.
const ANTIGRAVITY_MAIN_FAMILIES = ['claude', 'gemini 3.1', 'gemini 3.5'];

function isAntigravityMainModel(name: string): boolean {
  const raw = name.toLowerCase().replace(/-/g, ' ');
  const display = modelDisplayName(name).toLowerCase().replace(/-/g, ' ');
  return ANTIGRAVITY_MAIN_FAMILIES.some(f => raw.includes(f) || display.includes(f));
  }

function antigravityQuotaGroup(name: string): string {
  const display = modelDisplayName(name).toLowerCase().replace(/-/g, ' ');
  if (display.includes('gemini 3.5 flash')) return 'Gemini 3.5 Flash';
  if (display.includes('gemini 3.1 flash image')) return 'Gemini 3.1 Flash Image';
  if (display.includes('gemini 3.1 flash lite')) return 'Gemini 3.1 Flash Lite';
  if (display.includes('gemini 3.1 flash')) return 'Gemini 3.1 Flash';
  if (display.includes('gemini 3.1 pro')) return 'Gemini 3.1 Pro';
  if (display.includes('claude')) return 'Claude';
  return modelDisplayName(name);
  }

function aggregateQuotas(quotas: QuotaItem[]): QuotaItem[] {
  const groups = new Map<string, { used: number; total: number; unlimited: boolean; reset_at?: string }>();
  for (const q of quotas) {
    const key = antigravityQuotaGroup(q.name);
    const existing = groups.get(key);
    if (!existing) {
      groups.set(key, { used: q.used, total: q.total, unlimited: q.unlimited, reset_at: q.reset_at });
    } else {
      existing.used += q.used;
      existing.total += q.total;
      if (!q.unlimited) existing.unlimited = false;
      if (q.reset_at && (!existing.reset_at || q.reset_at < existing.reset_at)) {
        existing.reset_at = q.reset_at;
      }
    }
  }
  return Array.from(groups.entries()).map(([name, g]) => ({
    name,
    used: g.used,
    total: g.total,
    remaining_pct: g.total > 0 ? ((g.total - g.used) / g.total) * 100 : 0,
    unlimited: g.unlimited,
    reset_at: g.reset_at,
  }));
}

function visibleQuotas(item: QuotaCacheEntry): QuotaItem[] {
	const quotas = item.quotas ?? [];
	if (item.provider_id !== 'ag') return quotas;
	const filtered = quotas.filter(q => isAntigravityMainModel(q.name));
	return aggregateQuotas(filtered);
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
    if (p.includes('free')) return { cls: 'border-muted text-muted-foreground', icon: UserIcon };
    if (p.includes('plus') || p.includes('pro')) return { cls: 'border-emerald-500/30 text-emerald-400', icon: ZapIcon };
    if (p.includes('ultra') || p.includes('premium')) return { cls: 'border-violet-500/30 text-violet-400', icon: ZapIcon };
    return { cls: 'border-muted text-muted-foreground', icon: ZapIcon };
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
          <XIcon class="size-3.5" /> Clear
        </Button>
      {/if}
      <Button
        variant="outline"
        size="sm"
        onclick={handleRefreshAll}
        disabled={$quotaLoading || $quotaItems.length === 0}
        class="gap-1.5 text-body-sm rounded-sm cursor-pointer"
      >
        <RefreshCwIcon class="size-3.5" /> Refresh Page
      </Button>
  </div>
</div>

<!-- Summary -->
{#if $quotaSummary.length > 0}
<section class="grid grid-cols-1 sm:grid-cols-2 gap-4">
  <Card class="shadow-card">
    <CardContent class="p-4 flex items-center gap-3">
      <DollarSignIcon class="size-5 text-emerald-400" />
      <div>
        <p class="text-caption text-muted-foreground uppercase">Saved this month</p>
        <p class="text-display-md">{formatCost($quotaSavings)}</p>
      </div>
    </CardContent>
  </Card>
  <Card class="shadow-card">
    <CardContent class="p-4 flex items-center gap-3">
      <TimerIcon class="size-5 text-amber-400" />
      <div>
        <p class="text-caption text-muted-foreground uppercase">Next quota reset</p>
        <p class="text-display-md">{$quotaNextReset ? formatResetTime($quotaNextReset) : '—'}</p>
      </div>
    </CardContent>
  </Card>
</section>
{/if}

<!-- Filters -->
  <section class="rounded-xl bg-card p-4 shadow-card md:p-5">
    <div class="flex flex-col gap-4">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div class="relative w-full lg:max-w-md">
          <SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
          <Input
            type="text"
            bind:value={searchQuery}
            oninput={onSearchInput}
            placeholder="Search connections…"
            class="h-10 pl-9 text-body-sm"
          />
          {#if searchQuery}
            <button
              type="button"
              class="absolute inset-y-0 right-2 text-caption text-muted-foreground hover:text-foreground cursor-pointer"
              onclick={() => { searchQuery = ''; applyFilters(); }}
            >Clear</button>
          {/if}
        </div>
        <div class="flex items-center gap-2 text-caption-mono text-muted-foreground">
          <span>{$quotaTotal} connections</span>
        </div>
      </div>

      <!-- Status pills -->
      <div class="flex flex-wrap gap-1.5 border-t border-border pt-3">
        {#each statusOptions as opt}
          <button
            class="inline-flex items-center rounded-full px-3 py-1 text-caption font-medium transition-colors cursor-pointer
              {filterStatus === opt.value
                ? 'bg-foreground text-background'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
            onclick={() => onStatusChange(opt.value)}
          >
            {opt.label}
          </button>
        {/each}
      </div>

      <!-- Provider pills -->
      {#if $quotaSummary.length > 0}
        <div class="flex flex-wrap gap-1.5">
          <button
            class="inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-caption font-medium transition-colors cursor-pointer
              {filterProvider === ''
                ? 'bg-foreground text-background'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
            onclick={() => onProviderChange('')}
          >
            All
            <span class="font-mono opacity-75">{$quotaTotal}</span>
          </button>
          {#each $quotaSummary as ps}
            <button
              class="inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-caption font-medium transition-colors cursor-pointer
                {filterProvider === ps.provider_id
                  ? 'bg-foreground text-background'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
              onclick={() => onProviderChange(filterProvider === ps.provider_id ? '' : ps.provider_id)}
            >
              <span class="size-2 rounded-full shrink-0" style="background-color: {ps.provider_id === 'ag' ? '#4285f4' : ps.provider_id === 'cx' ? '#10a37f' : '#888'}"></span>
              {ps.display_name}
              <span class="font-mono opacity-75">{ps.total}</span>
            </button>
          {/each}
        </div>
      {/if}
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
        <AlertCircleIcon class="size-6 text-rose-400" />
        <p class="text-body-sm text-muted-foreground">{$quotaError}</p>
        <Button variant="outline" onclick={() => applyFilters()} class="text-body-sm rounded-sm cursor-pointer">Retry</Button>
      </CardContent>
    </Card>
  {:else if $quotaItems.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12 gap-3">
        <GaugeIcon class="size-6 text-muted-foreground" />
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
        {@const refreshing = isRefreshing(item.connection_id)}
        {@const planBadge = item.plan ? getPlanBadge(item.plan) : null}
        {@const expiry = item.auth_type === 'oauth' ? getTokenExpiry(item.oauth_expires_at) : null}
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
              <RefreshCwIcon class="size-3.5 {refreshing ? 'animate-spin' : ''}" />
            </button>
          </div>

          <!-- Meta row -->
          <div class="flex items-center gap-2 px-4 pb-3">
            <span class="inline-flex items-center gap-1.5 text-caption font-medium {st.cls}">
              <span class="size-1.5 rounded-full {st.dot}"></span>
              {st.label}
            </span>
            {#if planBadge}
              <span class="inline-flex items-center gap-1 text-caption {planBadge.cls}">
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
                <AlertCircleIcon class="size-3.5 text-rose-400 shrink-0 mt-0.5" />
                <p class="text-caption text-rose-400/80 leading-snug">{item.error}</p>
              </div>
            {:else if visibleQuotas(item).length === 0}
              <p class="text-caption text-muted-foreground">No quota data.</p>
            {:else}
              {#each visibleQuotas(item) as qi}
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
                          <ClockIcon class="size-2.5" />{formatResetTime(qi.reset_at)}
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
            {#if expiry}
              {#if expiry.status === 'expired'}
                <span class="text-caption text-red-400 flex items-center gap-1"><AlertCircleIcon class="size-3" /> Token expired</span>
              {:else if expiry.status === 'expiring'}
                <span class="text-caption text-amber-400 flex items-center gap-1"><AlertTriangleIcon class="size-3" /> Token expires in {expiry.text}</span>
              {:else}
                <span class="text-caption text-emerald-400 flex items-center gap-1"><CheckCircle2Icon class="size-3" /> Token expires in {expiry.text}</span>
              {/if}
            {:else}
              <span class="text-caption text-muted-foreground">Updated {formatFetched(item.fetched_at)}</span>
            {/if}
          </div>
        </div>
      {/each}
    </div>

    <Pagination
      page={$quotaPage}
      totalPages={$quotaTotalPages}
      total={$quotaTotal}
      perPage={perPage}
      onChange={(p) => goToPage(p)}
      onPerPageChange={(n) => { perPage = n; applyFilters(1); }}
    />
  {/if}
</div>
