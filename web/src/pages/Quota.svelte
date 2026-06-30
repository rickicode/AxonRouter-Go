<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    quotaData,
    quotaLoading,
    quotaError,
    loadQuota,
    refreshConnectionQuota,
  } from '$lib/stores';
  import type { QuotaItem, ConnectionQuota, ProviderQuota } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Progress } from '$lib/components/ui/progress';
  import { RefreshCw, Gauge, AlertCircle, Clock, User, Building2, Zap } from '@lucide/svelte';

  let refreshAllLoading = $state(false);
  let intervalId: ReturnType<typeof setInterval> | undefined;
  // Per-connection refreshing state
  let refreshingIds = $state<Set<string>>(new Set());

  onMount(() => {
    document.title = 'Quota — AxonRouter';
    loadQuota();
    intervalId = setInterval(loadQuota, 300_000); // 5 min
  });

  onDestroy(() => {
    if (intervalId) clearInterval(intervalId);
  });

  function isRefreshing(connId: string): boolean {
    return refreshingIds.has(connId);
  }

  function setRefreshing(connId: string, val: boolean) {
    refreshingIds = new Set(refreshingIds);
    if (val) refreshingIds.add(connId);
    else refreshingIds.delete(connId);
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

  // Account type styling — matches AxonRouter planVariants
  function getPlanBadgeClass(plan: string): string {
    const p = plan.toLowerCase();
    if (p.includes('free') || p.includes('starter'))
      return 'border-zinc-500/40 bg-zinc-500/10 text-zinc-300';
    if (p.includes('plus') || p.includes('pro'))
      return 'border-emerald-500/40 bg-emerald-500/10 text-emerald-300';
    if (p.includes('ultra') || p.includes('premium') || p.includes('max'))
      return 'border-violet-500/40 bg-violet-500/10 text-violet-300';
    if (p.includes('enterprise') || p.includes('business') || p.includes('team'))
      return 'border-amber-500/40 bg-amber-500/10 text-amber-300';
    return 'border-zinc-500/35 bg-zinc-500/10 text-zinc-300';
  }

  function getPlanIcon(plan: string) {
    const p = plan.toLowerCase();
    if (p.includes('free') || p.includes('starter')) return User;
    if (p.includes('enterprise') || p.includes('business') || p.includes('team')) return Building2;
    return Zap;
  }

  function formatResetTime(iso?: string): string {
    if (!iso) return '';
    try {
      const date = new Date(iso);
      const now = new Date();
      const diffMs = date.getTime() - now.getTime();
      if (diffMs <= 0) return 'resetting soon';
      const hours = Math.floor(diffMs / 3_600_000);
      const mins = Math.floor((diffMs % 3_600_000) / 60_000);
      if (hours > 24) {
        const days = Math.floor(hours / 24);
        return `resets in ${days}d ${hours % 24}h`;
      }
      if (hours > 0) return `resets in ${hours}h ${mins}m`;
      return `resets in ${mins}m`;
    } catch {
      return '';
    }
  }

  function formatTimestamp(ms: number): string {
    if (!ms) return '';
    return new Date(ms).toLocaleTimeString();
  }

  async function handleRefreshAll() {
    refreshAllLoading = true;
    const allConns = $quotaData.flatMap(p => p.connections);

    // Max 3 concurrent
    for (let i = 0; i < allConns.length; i += 3) {
      const batch = allConns.slice(i, i + 3);
      await Promise.allSettled(
        batch.map(c => {
          setRefreshing(c.connection_id, true);
          return refreshConnectionQuota(c.connection_id).finally(() =>
            setRefreshing(c.connection_id, false)
          );
        })
      );
    }

    refreshAllLoading = false;
  }

  async function handleRefreshOne(connId: string) {
    setRefreshing(connId, true);
    await refreshConnectionQuota(connId);
    setRefreshing(connId, false);
  }

  // Group antigravity quotas by family
  function groupByFamily(quotas: QuotaItem[]): Map<string, QuotaItem[]> {
    const groups = new Map<string, QuotaItem[]>();
    for (const q of quotas) {
      const family = q.family || 'other';
      if (!groups.has(family)) groups.set(family, []);
      groups.get(family)!.push(q);
    }
    return groups;
  }

  function familyLabel(family: string): string {
    switch (family) {
      case 'gemini': return 'Gemini';
      case 'claude': return 'Claude';
      default: return 'Other';
    }
  }
</script>

<div class="flex flex-1 flex-col gap-8 p-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div class="space-y-1">
      <h1 class="text-display-sm">Quota Tracker.</h1>
      <p class="text-body-sm text-muted-foreground">
        Live upstream quota for OAuth connections.
      </p>
    </div>
    <Button
      variant="outline"
      size="sm"
      onclick={handleRefreshAll}
      disabled={refreshAllLoading || $quotaLoading}
      class="gap-2 cursor-pointer"
    >
      <RefreshCw class="size-4 {refreshAllLoading ? 'animate-spin' : ''}" />
      {refreshAllLoading ? 'Refreshing…' : 'Refresh All'}
    </Button>
  </div>

  {#if $quotaLoading && $quotaData.length === 0}
    <!-- Loading skeleton -->
    <div class="flex flex-col gap-6">
      {#each [1, 2] as _}
        <Card class="shadow-card">
          <CardHeader class="pb-3">
            <div class="h-5 w-32 bg-muted animate-pulse rounded"></div>
          </CardHeader>
          <CardContent class="space-y-3">
            <div class="h-20 bg-muted animate-pulse rounded"></div>
          </CardContent>
        </Card>
      {/each}
    </div>
  {:else if $quotaError && $quotaData.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16 gap-3">
        <AlertCircle class="size-8 text-rose-400" />
        <p class="text-body-sm text-muted-foreground">{$quotaError}</p>
        <Button variant="outline" size="sm" onclick={() => loadQuota()} class="cursor-pointer">Retry</Button>
      </CardContent>
    </Card>
  {:else if $quotaData.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16 gap-3">
        <Gauge class="size-8 text-muted-foreground" />
        <p class="text-body-sm text-muted-foreground">No OAuth connections found.</p>
        <p class="text-caption text-muted-foreground">Add a Codex, Antigravity, or Kiro connection to see quota data.</p>
      </CardContent>
    </Card>
  {:else}
    {#each $quotaData as provider (provider.provider_id)}
      <div class="space-y-4">
        <!-- Provider header -->
        <div class="flex items-center gap-3">
          <span class="size-3 rounded-full" style="background-color: {provider.color}"></span>
          <h2 class="text-display-sm">{provider.display_name}</h2>
          <Badge variant="secondary" class="text-caption-mono rounded-sm">
            {provider.connections.length} {provider.connections.length === 1 ? 'account' : 'accounts'}
          </Badge>
        </div>

        <!-- Connection cards -->
        <div class="grid gap-4 md:grid-cols-2">
          {#each provider.connections as conn (conn.connection_id)}
            {@const refreshing = isRefreshing(conn.connection_id)}
            <Card class="shadow-card transition-all hover:bg-accent/5 {refreshing ? 'ring-1 ring-primary/30' : ''}">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                <div class="space-y-1.5 min-w-0">
                  <CardTitle class="text-body-sm-strong truncate">{conn.connection_name}</CardTitle>
                  <!-- Account type badge — prominent, color-coded -->
                  {#if conn.plan}
                    {@const PlanIcon = getPlanIcon(conn.plan)}
                    <div class="inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 {getPlanBadgeClass(conn.plan)}">
                      <PlanIcon class="size-3" />
                      <span class="text-[10px] font-bold uppercase tracking-[0.14em]">{conn.plan}</span>
                    </div>
                  {/if}
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  class="size-8 shrink-0 cursor-pointer text-muted-foreground hover:text-foreground transition-colors"
                  onclick={() => handleRefreshOne(conn.connection_id)}
                  disabled={refreshing}
                  title="Refresh quota"
                  aria-label="Refresh quota"
                >
                  <RefreshCw class="size-3.5 {refreshing ? 'animate-spin' : ''}" />
                </Button>
              </CardHeader>
              <CardContent class="space-y-3">
                {#if conn.error}
                  <div class="flex items-start gap-2 rounded-md bg-rose-500/10 border border-rose-500/20 px-3 py-2">
                    <AlertCircle class="size-4 text-rose-400 shrink-0 mt-0.5" />
                    <p class="text-caption text-rose-300">{conn.error}</p>
                  </div>
                {:else if conn.message}
                  <p class="text-caption text-muted-foreground">{conn.message}</p>
                {:else if conn.quotas.length === 0}
                  <p class="text-caption text-muted-foreground">No quota data available.</p>
                {:else}
                  <!-- Provider-specific layouts -->
                  {#if provider.provider_id === 'cx'}
                    <!-- Codex: percentage bars (session + weekly + code review) -->
                    <div class="space-y-3">
                      {#each conn.quotas as qi}
                        <div class="space-y-1.5 rounded-md border border-zinc-800/80 bg-zinc-900/30 px-3 py-2">
                          <div class="flex items-center justify-between">
                            <span class="text-caption text-zinc-300">{qi.name}</span>
                            <span class="text-sm font-bold tabular-nums {quotaTextColor(qi.remaining_pct)}">
                              {qi.remaining_pct.toFixed(1)}%
                            </span>
                          </div>
                          <div class="h-2 rounded-full overflow-hidden bg-zinc-800">
                            <div
                              class="h-full rounded-full transition-all duration-500 {quotaBarColor(qi.remaining_pct)}"
                              style="width: {qi.remaining_pct}%"
                            ></div>
                          </div>
                          {#if qi.reset_at}
                            <div class="flex items-center gap-1 text-[10px] text-zinc-500">
                              <Clock class="size-3" />
                              <span>Reset in {formatResetTime(qi.reset_at)}</span>
                            </div>
                          {/if}
                        </div>
                      {/each}
                    </div>

                  {:else if provider.provider_id === 'ag'}
                    <!-- Antigravity: per-model compact bars grouped by family -->
                    {@const families = groupByFamily(conn.quotas)}
                    <div class="space-y-4">
                      {#each [...families.entries()] as [family, items]}
                        {@const familyRemaining = Math.max(...items.map(q => q.remaining_pct))}
                        {@const allExhausted = items.every(q => q.remaining_pct <= 0)}
                        <div>
                          <!-- Family header -->
                          <div class="flex items-center justify-between mb-1.5 px-1">
                            <span class="text-[11px] font-semibold text-zinc-300 uppercase tracking-wider">
                              {familyLabel(family)}
                            </span>
                            {#if allExhausted}
                              <span class="text-[10px] font-bold text-rose-400">Exhausted</span>
                            {:else}
                              <span class="text-[10px] font-bold tabular-nums {quotaTextColor(familyRemaining)}">
                                {familyRemaining.toFixed(0)}%
                              </span>
                            {/if}
                          </div>
                          <!-- Model bars — 2-column grid -->
                          <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
                            {#each items as qi}
                              <div class="min-w-0">
                                <div class="flex items-center justify-between gap-1 mb-0.5">
                                  <span class="text-[10px] font-medium text-zinc-400 truncate" title={qi.model_key || qi.name}>
                                    {qi.model_key || qi.name}
                                  </span>
                                  {#if qi.unlimited}
                                    <span class="text-[10px] font-bold text-emerald-400 shrink-0">∞</span>
                                  {:else}
                                    <span class="text-[10px] font-bold tabular-nums shrink-0 {quotaTextColor(qi.remaining_pct)}">
                                      {qi.remaining_pct.toFixed(0)}%
                                    </span>
                                  {/if}
                                </div>
                                {#if !qi.unlimited}
                                  <div class="h-1 rounded-full bg-zinc-800 overflow-hidden">
                                    <div
                                      class="h-full rounded-full transition-all duration-500 {quotaBarColor(qi.remaining_pct)}"
                                      style="width: {qi.remaining_pct}%"
                                    ></div>
                                  </div>
                                {/if}
                                {#if qi.reset_at}
                                  <span class="text-[8px] text-zinc-600">{formatResetTime(qi.reset_at)}</span>
                                {/if}
                              </div>
                            {/each}
                          </div>
                        </div>
                      {/each}
                    </div>

                  {:else if provider.provider_id === 'kiro'}
                    <!-- Kiro: credit-based with progress bars -->
                    <div class="space-y-3">
                      {#each conn.quotas as qi}
                        {@const usedPct = qi.total > 0 ? (qi.used / qi.total) * 100 : 0}
                        {@const remainingPct = 100 - usedPct}
                        <div class="space-y-1.5">
                          <div class="flex items-center justify-between">
                            <span class="text-caption text-zinc-300 capitalize">{qi.name}</span>
                            <div class="flex items-center gap-2">
                              {#if qi.unlimited}
                                <span class="text-caption-mono text-emerald-400">unlimited</span>
                              {:else}
                                <span class="text-xs font-bold tabular-nums {quotaTextColor(remainingPct)}">
                                  {(qi.total - qi.used).toFixed(0)}
                                </span>
                                <span class="text-[10px] text-zinc-500">/ {qi.total.toFixed(0)}</span>
                              {/if}
                            </div>
                          </div>
                          {#if !qi.unlimited}
                            <div class="h-1.5 rounded-full bg-zinc-800 overflow-hidden">
                              <div
                                class="h-full rounded-full transition-all duration-500 {quotaBarColor(remainingPct)}"
                                style="width: {usedPct}%"
                              ></div>
                            </div>
                          {/if}
                          {#if qi.reset_at}
                            <span class="text-[10px] text-zinc-500">resets in {formatResetTime(qi.reset_at)}</span>
                          {/if}
                        </div>
                      {/each}
                    </div>

                  {:else}
                    <!-- Generic fallback -->
                    <div class="space-y-3">
                      {#each conn.quotas as qi}
                        {@const usedPct = qi.total > 0 ? (qi.used / qi.total) * 100 : 0}
                        <div class="space-y-1.5">
                          <div class="flex items-center justify-between">
                            <span class="text-caption text-zinc-300 truncate">{qi.name}</span>
                            <span class="text-[10px] font-bold tabular-nums {quotaTextColor(qi.remaining_pct)}">
                              {qi.remaining_pct.toFixed(1)}%
                            </span>
                          </div>
                          <div class="h-1.5 rounded-full bg-zinc-800 overflow-hidden">
                            <div
                              class="h-full rounded-full transition-all duration-500 {quotaBarColor(qi.remaining_pct)}"
                              style="width: {usedPct}%"
                            ></div>
                          </div>
                          <div class="flex items-center justify-between text-[10px] text-zinc-500">
                            <span>{qi.used.toFixed(0)} / {qi.total > 0 ? qi.total.toFixed(0) : '∞'}</span>
                            {#if qi.reset_at}
                              <span>resets in {formatResetTime(qi.reset_at)}</span>
                            {/if}
                          </div>
                        </div>
                      {/each}
                    </div>
                  {/if}
                {/if}

                <!-- Fetched timestamp -->
                {#if conn.fetched_at}
                  <p class="text-caption text-muted-foreground/50 pt-1">
                    fetched {formatTimestamp(conn.fetched_at)}
                  </p>
                {/if}
              </CardContent>
            </Card>
          {/each}
        </div>
      </div>
    {/each}
  {/if}
</div>
