<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import ProviderOctopus from '$lib/components/ProviderOctopus.svelte';
  import { dashboardApi, type DashboardStats } from '$lib/api';
  import { formatTokens, formatCount, loadActiveRequests, activeRequests } from '$lib/stores';
  import { toast } from 'svelte-sonner';

  import ActivityIcon from '@lucide/svelte/icons/activity';
  import CpuIcon from '@lucide/svelte/icons/cpu';
  import DollarSignIcon from '@lucide/svelte/icons/dollar-sign';
  import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
  import TimerIcon from '@lucide/svelte/icons/timer';
  import ZapIcon from '@lucide/svelte/icons/zap';
  import DatabaseIcon from '@lucide/svelte/icons/database';
  import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
  import ServerIcon from '@lucide/svelte/icons/server';
  import BoxesIcon from '@lucide/svelte/icons/boxes';
  import LayersIcon from '@lucide/svelte/icons/layers';
  import ClockIcon from '@lucide/svelte/icons/clock';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';

  let stats = $state<DashboardStats | null>(null);
  let loading = $state(true);
  let errorMsg = $state<string | null>(null);

  function fmtInt(n: number): string {
    return n.toLocaleString();
  }
  function fmtPercent(n: number): string {
    return (n * 100).toFixed(1) + '%';
  }
  function money(n: number): string {
    return n >= 1 ? '$' + n.toFixed(2) : '$' + n.toFixed(4);
  }
  function fmtLatency(n: number): string {
    return n >= 1000 ? (n / 1000).toFixed(2) + 's' : Math.round(n) + 'ms';
  }
  function fmtUptime(seconds: number): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  let activeProviderList = $derived(
    Array.from(new Set(($activeRequests || []).map((r) => r.provider_type_id))).map((id) => ({
      id,
      connection_count: 0,
    }))
  );
  let activeProviderIds = $derived(($activeRequests || []).map((r) => r.provider_type_id));
  let streamCount = $derived(($activeRequests || []).length);

  async function load() {
    loading = true;
    errorMsg = null;
    try {
      const s = await dashboardApi.stats();
      stats = s;
      await loadActiveRequests();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Failed to load dashboard';
      toast.error(errorMsg);
    } finally {
      loading = false;
    }
  }

  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let activeTimer: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    document.title = 'Dashboard — AxonRouter';
    load();
    refreshTimer = setInterval(load, 10000);
    activeTimer = setInterval(loadActiveRequests, 3000);
    return () => {
      if (refreshTimer) clearInterval(refreshTimer);
      if (activeTimer) clearInterval(activeTimer);
    };
  });
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
    <div class="space-y-1">
      <h1 class="text-display-lg">Dashboard.</h1>
      <p class="text-body-sm text-muted-foreground">Real-time overview of traffic, cost, and system health.</p>
    </div>
    <div class="flex items-center gap-2">
      <Button variant="outline" size="sm" onclick={load} class="text-body-sm rounded-sm cursor-pointer gap-1.5">
        <RefreshCwIcon class="size-3.5" />Refresh
      </Button>
      <a href="/usage">
        <Button variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer gap-1.5">
          View full usage<ArrowRightIcon class="size-3.5" />
        </Button>
      </a>
    </div>
  </div>

  {#if loading && !stats}
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {#each Array(12) as _}
        <div class="h-28 bg-muted animate-pulse rounded-xl"></div>
      {/each}
    </div>
  {:else if errorMsg && !stats}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16 gap-4">
        <p class="text-body-sm text-muted-foreground">{errorMsg}</p>
        <Button onclick={load} variant="outline" class="text-body-sm cursor-pointer">Try again</Button>
      </CardContent>
    </Card>
  {:else if stats}
    <!-- Today KPIs -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-5">
      {@render kpiCard('Requests', formatCount(stats.requests_today), 'today', ActivityIcon, 'bg-blue-500/40', 'text-blue-400')}
      {@render kpiCard('Tokens', formatTokens(stats.tokens_today), 'today', CpuIcon, 'bg-violet-500/40', 'text-violet-400')}
      {@render kpiCard('Cost', money(stats.cost_today), 'today', DollarSignIcon, 'bg-emerald-500/40', 'text-emerald-400')}
      {@render kpiCard('Errors', formatCount(stats.errors_today), 'today', AlertTriangleIcon, 'bg-red-500/40', 'text-red-400')}
      {@render kpiCard('Avg latency', fmtLatency(stats.avg_latency_ms_today), 'today', TimerIcon, 'bg-amber-500/40', 'text-amber-400')}
    </div>

    <!-- System KPIs -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {@render kpiCard('CPU', fmtPercent(stats.cpu_percent), 'system load', ZapIcon, 'bg-cyan-500/40', 'text-cyan-400')}
      {@render kpiCard('Memory', fmtPercent(stats.memory_percent), 'system memory', DatabaseIcon, 'bg-fuchsia-500/40', 'text-fuchsia-400')}
      {@render kpiCard('Disk', fmtPercent(stats.disk_percent), 'system disk', HardDriveIcon, 'bg-orange-500/40', 'text-orange-400')}
      {@render kpiCard('Connections', `${stats.healthy_connections ?? 0}/${stats.total_connections}`, 'healthy / total', ServerIcon, 'bg-lime-500/40', 'text-lime-400')}
      {@render kpiCard('Providers', fmtInt(stats.total_providers), 'registered', BoxesIcon, 'bg-pink-500/40', 'text-pink-400')}
      {@render kpiCard('Combos', fmtInt(stats.total_combos), 'configured', LayersIcon, 'bg-indigo-500/40', 'text-indigo-400')}
      {@render kpiCard('Uptime', fmtUptime(stats.uptime_seconds), 'since start', ClockIcon, 'bg-sky-500/40', 'text-sky-400')}
    </div>

    <!-- Provider octopus mini -->
    <Card class="shadow-card">
      <CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between">
        <div class="flex items-center gap-2">
          <ActivityIcon class="size-4 text-muted-foreground" />
          <CardTitle class="text-body-md-strong">Provider network</CardTitle>
        </div>
        <a href="/usage">
          <Button variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer gap-1.5">
            View full usage<ArrowRightIcon class="size-3.5" />
          </Button>
        </a>
      </CardHeader>
      <CardContent class="pt-4">
        <ProviderOctopus providers={activeProviderList} activeIds={activeProviderIds} streamCount={streamCount} />
      </CardContent>
    </Card>
  {/if}
</div>

{#snippet kpiCard(label: string, value: string, sub: string, Icon: any, borderClass: string, iconClass: string)}
<Card class="shadow-card relative overflow-hidden">
  <div class="absolute top-0 left-0 w-full h-0.5 {borderClass}"></div>
  <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-1 pt-5 px-5">
    <CardTitle class="text-caption-mono text-muted-foreground uppercase">{label}</CardTitle>
    <Icon class="size-4 {iconClass}" />
  </CardHeader>
  <CardContent class="px-5 pb-5">
    <div class="text-display-md font-semibold text-foreground tabular-nums">{value}</div>
    <p class="text-caption text-muted-foreground mt-1">{sub}</p>
  </CardContent>
</Card>
{/snippet}
