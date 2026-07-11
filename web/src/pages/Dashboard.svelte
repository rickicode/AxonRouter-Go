<script lang="ts">
  import { onMount } from 'svelte';
  import { loadDashboardStats, dashboardStats, usageStats, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import ChartBar from '$lib/components/ChartBar.svelte';

  onMount(() => {
    document.title = 'Dashboard — AxonRouter';
    loadDashboardStats();
  });

  function fmtNum(n: number): string {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
    return n.toLocaleString();
  }

  function fmtCost(n: number): string {
    if (n >= 1) return '$' + n.toFixed(2);
    if (n >= 0.01) return '$' + n.toFixed(3);
    return '$' + n.toFixed(4);
  }

  function fmtTokens(n: number): string {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
    return n.toLocaleString();
  }

  // Compute totals from daily data for trend indicators
  let totals = $derived.by(() => {
    const daily = $usageStats.daily;
    if (daily.length === 0) return { totalReq: 0, totalTokens: 0, totalCost: 0, totalErrors: 0 };
    return daily.reduce(
      (acc, d) => ({
        totalReq: acc.totalReq + d.requests,
        totalTokens: acc.totalTokens + d.tokens,
        totalCost: acc.totalCost + d.cost_usd,
        totalErrors: acc.totalErrors + d.errors,
      }),
      { totalReq: 0, totalTokens: 0, totalCost: 0, totalErrors: 0 }
    );
  });

  // Provider max for bar width normalization
  let providerMax = $derived(
    Math.max(...$usageStats.providers.map((p) => p.requests), 1)
  );
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <div class="space-y-1">
      <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
      <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
    </div>
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
      {#each Array(3) as _}
        <div class="h-32 bg-muted animate-pulse rounded-xl"></div>
      {/each}
    </div>
    <div class="h-64 bg-muted animate-pulse rounded-xl"></div>
  {:else if $error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadDashboardStats} variant="outline" class="text-body-sm cursor-pointer">Try again</Button>
      </CardContent>
    </Card>
  {:else if $dashboardStats}
    <!-- Header -->
    <div class="space-y-1">
      <h1 class="text-display-lg">Dashboard.</h1>
      <p class="text-body-sm text-muted-foreground">Request, token, and cost analytics across all providers.</p>
    </div>

    <!-- Summary Cards -->
    <div class="grid gap-4 md:grid-cols-4">
      <Card class="shadow-card relative overflow-hidden">
        <div class="absolute top-0 left-0 w-full h-0.5 bg-pink-500/40"></div>
        <CardHeader class="pb-1 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Requests Today</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-foreground tabular-nums">
            {fmtNum($dashboardStats.total_requests_today)}
          </div>
          <p class="text-caption text-muted-foreground mt-1">
            {$dashboardStats.total_connections} connections · {$dashboardStats.active_connections} active
          </p>
        </CardContent>
      </Card>

      <Card class="shadow-card relative overflow-hidden">
        <div class="absolute top-0 left-0 w-full h-0.5 bg-violet-500/40"></div>
        <CardHeader class="pb-1 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Tokens Today</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-pink-400 tabular-nums">
            {fmtTokens($dashboardStats.tokens_today)}
          </div>
          <p class="text-caption text-muted-foreground mt-1">input + output combined</p>
        </CardContent>
      </Card>

      <Card class="shadow-card relative overflow-hidden">
        <div class="absolute top-0 left-0 w-full h-0.5 bg-emerald-500/40"></div>
        <CardHeader class="pb-1 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Usage Cost</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-emerald-400 tabular-nums">
            {fmtCost($dashboardStats.cost_today)}
          </div>
          <p class="text-caption text-muted-foreground mt-1">today's total spend</p>
        </CardContent>
      </Card>

      <Card class="shadow-card relative overflow-hidden">
        <div class="absolute top-0 left-0 w-full h-0.5 bg-red-500/40"></div>
        <CardHeader class="pb-1 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Error Rate</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold tabular-nums {totals.totalErrors > 0 ? 'text-red-400' : 'text-muted-foreground'}">
            {totals.totalReq > 0 ? ((totals.totalErrors / totals.totalReq) * 100).toFixed(1) : '0.0'}%
          </div>
          <p class="text-caption text-muted-foreground mt-1">{totals.totalErrors} errors · 30d window</p>
        </CardContent>
      </Card>
    </div>

    <!-- Charts -->
    {#if $usageStats.daily.length > 0}
      <div class="grid gap-4 lg:grid-cols-2">
        <ChartBar
          data={$usageStats.daily.map((d) => ({ date: d.date, value: d.requests, errors: d.errors }))}
          title="Daily Requests (30d)"
          color="#ec4899"
          color2="#f472b6"
          height={240}
          showErrors
        />
        <ChartBar
          data={$usageStats.daily.map((d) => ({ date: d.date, value: d.tokens }))}
          title="Daily Tokens (30d)"
          color="#a78bfa"
          color2="#c4b5fd"
          height={240}
        />
      </div>
      <ChartBar
        data={$usageStats.daily.map((d) => ({ date: d.date, value: d.cost_usd }))}
        title="Daily Cost (30d)"
        valuePrefix="$"
        color="#10b981"
        color2="#34d399"
 height={240}
      />
    {:else}
      <Card class="shadow-card">
        <CardContent class="py-12 text-center text-muted-foreground text-body-sm">
          No usage data yet. Usage charts will appear once requests are logged.
        </CardContent>
      </Card>
    {/if}

    <!-- Provider breakdown -->
    {#if $usageStats.providers.length > 0}
      <div class="space-y-3">
        <h2 class="text-display-sm">Provider Usage.</h2>
        <Card class="shadow-card overflow-hidden">
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-border">
                <th class="text-left px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Provider</th>
                <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Requests</th>
                <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Tokens</th>
                <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Cost</th>
                <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Errors</th>
              </tr>
            </thead>
            <tbody>
              {#each $usageStats.providers as p (p.provider_type_id)}
                <tr class="border-b border-border/50 last:border-0 hover:bg-accent/5 transition-colors">
                  <td class="px-5 py-3 font-medium">{p.provider_type_id}</td>
                  <td class="text-right px-5 py-3 tabular-nums">{fmtNum(p.requests)}</td>
                  <td class="text-right px-5 py-3 tabular-nums text-pink-400">{fmtTokens(p.total_tokens)}</td>
                  <td class="text-right px-5 py-3 tabular-nums text-emerald-400">{fmtCost(p.cost_usd)}</td>
                  <td class="text-right px-5 py-3 tabular-nums {p.errors > 0 ? 'text-red-400' : 'text-muted-foreground'}">{p.errors}</td>
                </tr>
                <!-- Visual bar showing relative request volume -->
                <tr class="border-b border-border/50 last:border-0">
                  <td colspan="5" class="px-5 pb-3 pt-0">
                    <div class="h-1 rounded-full bg-muted overflow-hidden">
                      <div class="h-full rounded-full bg-pink-500/60 transition-all" style="width: {Math.max((p.requests / providerMax) * 100, 2)}%"></div>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </Card>
      </div>
    {/if}
  {/if}
</div>
