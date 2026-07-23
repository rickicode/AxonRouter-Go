<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { dashboardApi, type DashboardStats } from '$lib/api';
  import { formatTokens, formatCount, formatBytes, loadActiveRequests, activeRequests } from '$lib/stores';
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
  import RadioIcon from '@lucide/svelte/icons/radio';

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

  let streamCount = $derived(($activeRequests || []).length);

  function connectionSub(stats: DashboardStats): string {
    const total = stats.total_connections ?? 0;
    const healthy = stats.healthy_connections ?? 0;
    const unhealthy = total - healthy;
    if (unhealthy === 0) return 'all healthy';
    return `${fmtInt(unhealthy)} ${unhealthy === 1 ? 'error' : 'errors'}`;
  }

  const STATUS_COLORS: Record<string, string> = {
    ready: 'bg-green-500',
    rate_limited: 'bg-yellow-500',
    quota_exhausted: 'bg-orange-500',
    disabled: 'bg-zinc-400',
  };

  function statusDistribution(stats: DashboardStats): { status: string; count: number; color: string }[] {
    const counts = stats.status_counts ?? {};
    return Object.entries(counts)
      .filter(([, count]) => count > 0)
      .map(([status, count]) => ({ status, count, color: STATUS_COLORS[status] ?? 'bg-zinc-500' }))
      .sort((a, b) => b.count - a.count);
  }

  async function load() {
    loading = true;
    errorMsg = null;
    try {
      stats = await dashboardApi.stats();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Failed to load dashboard';
      toast.error(errorMsg);
    } finally {
      loading = false;
    }
    // Best-effort live stream indicator; don't let it fail the whole dashboard.
    try {
      await loadActiveRequests();
    } catch {
      // ignored
    }
  }

  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let activeTimer: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    document.title = 'Dashboard — AxonRouter';
    load();
    refreshTimer = setInterval(load, 5000);
    activeTimer = setInterval(loadActiveRequests, 3000);
    return () => {
      if (refreshTimer) clearInterval(refreshTimer);
      if (activeTimer) clearInterval(activeTimer);
    };
  });
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Dashboard.</h1>
    <p class="text-body-sm text-muted-foreground">Auto-refreshing overview of traffic, cost, and system health.</p>
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
      {@render kpiCard('CPU', fmtPercent(stats.cpu_percent), `${stats.cpu_cores ?? 0} cores`, ZapIcon, 'bg-cyan-500/40', 'text-cyan-400')}
      {@render kpiCard('Memory', `${formatBytes(stats.memory_used_bytes ?? 0)} / ${formatBytes(stats.memory_total_bytes ?? 0)}`, fmtPercent(stats.memory_percent), DatabaseIcon, 'bg-fuchsia-500/40', 'text-fuchsia-400')}
      {@render kpiCard('Disk', `${formatBytes(stats.disk_used_bytes ?? 0)} / ${formatBytes(stats.disk_total_bytes ?? 0)}`, fmtPercent(stats.disk_percent), HardDriveIcon, 'bg-orange-500/40', 'text-orange-400')}
      {@render kpiCard('Connections', `${stats.healthy_connections ?? 0}/${stats.total_connections}`, connectionSub(stats), ServerIcon, 'bg-lime-500/40', 'text-lime-400')}
      {@render kpiCard('Providers', fmtInt(stats.total_providers), 'registered', BoxesIcon, 'bg-pink-500/40', 'text-pink-400')}
      {@render kpiCard('Combos', fmtInt(stats.total_combos), 'configured', LayersIcon, 'bg-indigo-500/40', 'text-indigo-400')}
      {@render kpiCard('Uptime', fmtUptime(stats.uptime_seconds), 'since start', ClockIcon, 'bg-sky-500/40', 'text-sky-400')}
      {@render kpiCard('Active requests', formatCount(streamCount), 'live', RadioIcon, 'bg-teal-500/40', 'text-teal-400')}
    </div>

    <!-- Connection status chart -->
    <Card class="shadow-card">
      <CardHeader class="pb-3 border-b border-border">
        <div class="flex items-center gap-2">
          <ServerIcon class="size-4 text-muted-foreground" />
          <CardTitle class="text-body-md-strong">Connection status</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="pt-4">
        {#if Object.keys(stats.status_counts ?? {}).length === 0}
          <p class="text-body-sm text-muted-foreground py-8 text-center">No connection status data.</p>
        {:else}
          {@const dist = statusDistribution(stats)}
          {@const total = dist.reduce((sum, d) => sum + d.count, 0)}
          <div class="flex h-4 w-full overflow-hidden rounded-full bg-muted">
            {#each dist as s}
              <div class="{s.color} h-full" style="width: {(s.count / total) * 100}%;" title="{s.status}: {s.count}"></div>
            {/each}
          </div>
          <div class="mt-4 flex flex-wrap gap-3">
            {#each dist as s}
              <div class="flex items-center gap-1.5">
                <span class="inline-block size-3 rounded-full {s.color}"></span>
                <span class="text-body-sm text-muted-foreground capitalize">{s.status.replace(/_/g, ' ')}</span>
                <span class="text-body-sm-strong tabular-nums">{fmtInt(s.count)}</span>
              </div>
            {/each}
          </div>
        {/if}
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
