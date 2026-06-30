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
  import { RefreshCw, Gauge, AlertCircle, Clock } from '@lucide/svelte';

  let refreshAllLoading = $state(false);
  let intervalId: ReturnType<typeof setInterval> | undefined;

  onMount(() => {
    document.title = 'Quota — AxonRouter';
    loadQuota();
    intervalId = setInterval(loadQuota, 300_000); // 5 min
  });

  onDestroy(() => {
    if (intervalId) clearInterval(intervalId);
  });

  function quotaColor(pct: number): string {
    if (pct > 50) return 'bg-emerald-500';
    if (pct > 20) return 'bg-amber-500';
    return 'bg-rose-500';
  }

  function quotaTextColor(pct: number): string {
    if (pct > 50) return 'text-emerald-400';
    if (pct > 20) return 'text-amber-400';
    return 'text-rose-400';
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
    let ok = 0;
    let failed = 0;

    // Max 3 concurrent
    for (let i = 0; i < allConns.length; i += 3) {
      const batch = allConns.slice(i, i + 3);
      const results = await Promise.allSettled(
        batch.map(c => refreshConnectionQuota(c.connection_id))
      );
      for (const r of results) {
        if (r.status === 'fulfilled' && r.value) ok++;
        else failed++;
      }
    }

    refreshAllLoading = false;
    if (failed > 0) {
      // toast already fired per-connection
    }
  }

  async function handleRefreshOne(connId: string) {
    await refreshConnectionQuota(connId);
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
      class="gap-2"
    >
      <RefreshCw class="size-4 {refreshAllLoading ? 'animate-spin' : ''}" />
      Refresh All
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
        <Button variant="outline" size="sm" onclick={() => loadQuota()}>Retry</Button>
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
            {provider.connections.length} {provider.connections.length === 1 ? 'conn' : 'conns'}
          </Badge>
        </div>

        <!-- Connection cards -->
        <div class="grid gap-4 md:grid-cols-2">
          {#each provider.connections as conn (conn.connection_id)}
            <Card class="shadow-card transition-all hover:bg-accent/5">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                <div class="space-y-1 min-w-0">
                  <CardTitle class="text-body-sm-strong truncate">{conn.connection_name}</CardTitle>
                  {#if conn.plan}
                    <Badge variant="outline" class="text-caption-mono rounded-sm">
                      {conn.plan}
                    </Badge>
                  {/if}
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  class="size-7 shrink-0"
                  onclick={() => handleRefreshOne(conn.connection_id)}
                  title="Refresh"
                >
                  <RefreshCw class="size-3.5" />
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
                    <!-- Codex: percentage bars -->
                    <div class="space-y-3">
                      {#each conn.quotas as qi}
                        <div class="space-y-1.5">
                          <div class="flex items-center justify-between">
                            <span class="text-caption text-muted-foreground">{qi.name}</span>
                            <span class="text-caption-mono {quotaTextColor(qi.remaining_pct)}">
                              {qi.remaining_pct.toFixed(1)}% remaining
                            </span>
                          </div>
                          <Progress
                            value={qi.used}
                            max={qi.total}
                            class="h-2 bg-zinc-800"
                          />
                          {#if qi.reset_at}
                            <div class="flex items-center gap-1 text-caption text-muted-foreground">
                              <Clock class="size-3" />
                              {formatResetTime(qi.reset_at)}
                            </div>
                          {/if}
                        </div>
                      {/each}
                    </div>

                  {:else if provider.provider_id === 'ag'}
                    <!-- Antigravity: per-model bars grouped by family -->
                    {@const families = groupByFamily(conn.quotas)}
                    <div class="space-y-4">
                      {#each [...families.entries()] as [family, items]}
                        <div class="space-y-2">
                          <p class="text-caption-mono text-muted-foreground uppercase tracking-wider">
                            {familyLabel(family)}
                          </p>
                          <div class="space-y-2">
                            {#each items as qi}
                              <div class="space-y-1">
                                <div class="flex items-center justify-between">
                                  <span class="text-caption truncate max-w-[180px]" title={qi.model_key || qi.name}>
                                    {qi.model_key || qi.name}
                                  </span>
                                  {#if qi.unlimited}
                                    <span class="text-caption-mono text-emerald-400">unlimited</span>
                                  {:else}
                                    <span class="text-caption-mono {quotaTextColor(qi.remaining_pct)}">
                                      {qi.remaining_pct.toFixed(1)}%
                                    </span>
                                  {/if}
                                </div>
                                {#if !qi.unlimited}
                                  <Progress
                                    value={qi.used}
                                    max={qi.total}
                                    class="h-1.5 bg-zinc-800"
                                  />
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
                        <div class="space-y-1.5">
                          <div class="flex items-center justify-between">
                            <span class="text-caption text-muted-foreground capitalize">{qi.name}</span>
                            <span class="text-caption-mono">
                              {#if qi.unlimited}
                                <span class="text-emerald-400">unlimited</span>
                              {:else}
                                <span class="{quotaTextColor(qi.remaining_pct)}">{qi.used.toFixed(0)}</span>
                                <span class="text-muted-foreground"> / {qi.total.toFixed(0)}</span>
                              {/if}
                            </span>
                          </div>
                          {#if !qi.unlimited}
                            <Progress
                              value={qi.used}
                              max={qi.total}
                              class="h-2 bg-zinc-800"
                            />
                          {/if}
                          {#if qi.reset_at}
                            <div class="flex items-center gap-1 text-caption text-muted-foreground">
                              <Clock class="size-3" />
                              {formatResetTime(qi.reset_at)}
                            </div>
                          {/if}
                        </div>
                      {/each}
                    </div>

                  {:else}
                    <!-- Generic fallback -->
                    <div class="space-y-3">
                      {#each conn.quotas as qi}
                        <div class="space-y-1.5">
                          <div class="flex items-center justify-between">
                            <span class="text-caption text-muted-foreground">{qi.name}</span>
                            <span class="text-caption-mono {quotaTextColor(qi.remaining_pct)}">
                              {qi.remaining_pct.toFixed(1)}%
                            </span>
                          </div>
                          <Progress
                            value={qi.used}
                            max={qi.total}
                            class="h-2 bg-zinc-800"
                          />
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
