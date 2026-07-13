<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { usageApi, dashboardApi, type UsageData } from '$lib/api';
import { formatTokens, formatCount } from '$lib/stores';
  import { toast } from 'svelte-sonner';

  import ActivityIcon from '@lucide/svelte/icons/activity';
  import CpuIcon from '@lucide/svelte/icons/cpu';
  import DollarSignIcon from '@lucide/svelte/icons/dollar-sign';
  import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
  import TimerIcon from '@lucide/svelte/icons/timer';
  import ServerIcon from '@lucide/svelte/icons/server';
  import BoxesIcon from '@lucide/svelte/icons/boxes';
  import LayersIcon from '@lucide/svelte/icons/layers';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

  type SysStats = {
    total_providers: number;
    total_connections: number;
    total_combos: number;
    status_counts: Record<string, number>;
    requests_today: number;
    tokens_today: number;
    cost_today: number;
    uptime_seconds: number;
    healthy_connections: number;
  };

  let usage = $state<UsageData | null>(null);
  let sys = $state<SysStats | null>(null);
  let loading = $state(true);
  let errorMsg = $state<string | null>(null);

  let from = $state(daysAgo(29));
  let to = $state(today());
  let range = $state<'7d' | '30d' | 'month'>('30d');

  let trafficCanvas = $state<HTMLCanvasElement | null>(null);
  let costCanvas = $state<HTMLCanvasElement | null>(null);
  let providerCanvas = $state<HTMLCanvasElement | null>(null);
  let statusCanvas = $state<HTMLCanvasElement | null>(null);
  let trafficChart = $state<any>(null);
  let costChart = $state<any>(null);
  let providerChart = $state<any>(null);
  let statusChart = $state<any>(null);

  function today(): string {
    return new Date().toISOString().split('T')[0];
  }
  function daysAgo(n: number): string {
    const d = new Date();
    d.setDate(d.getDate() - n);
    return d.toISOString().split('T')[0];
  }
  function startOfMonth(): string {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
  }
  function fmtInt(n: number): string {
    return n.toLocaleString();
  }
  function fmtPercent(n: number): string {
    return (n * 100).toFixed(1) + '%';
  }
  function fmtMs(n: number): string {
    return n >= 1000 ? (n / 1000).toFixed(2) + 's' : Math.round(n) + 'ms';
  }
  function money(n: number): string {
    return n >= 1 ? '$' + n.toFixed(2) : '$' + n.toFixed(4);
  }

  const accents: Record<string, { border: string; text: string }> = {
    pink: { border: 'bg-pink-500/40', text: 'text-pink-400' },
    violet: { border: 'bg-violet-500/40', text: 'text-violet-400' },
    emerald: { border: 'bg-emerald-500/40', text: 'text-emerald-400' },
    red: { border: 'bg-red-500/40', text: 'text-red-400' },
    amber: { border: 'bg-amber-500/40', text: 'text-amber-400' },
    blue: { border: 'bg-blue-500/40', text: 'text-blue-400' },
    cyan: { border: 'bg-cyan-500/40', text: 'text-cyan-400' },
    fuchsia: { border: 'bg-fuchsia-500/40', text: 'text-fuchsia-400' },
  };

  let cards = $derived.by(() => {
    const s = usage?.summary;
    return [
      { label: 'Requests', value: s ? formatCount(s.requests) : '0', sub: 'in selected period', icon: ActivityIcon, accent: 'pink' },
      { label: 'Total Tokens', value: s ? formatTokens(s.total_tokens) : '0', sub: 'in selected period', icon: CpuIcon, accent: 'violet' },
      { label: 'Total Cost', value: s ? money(s.cost_usd) : '$0', sub: 'in selected period', icon: DollarSignIcon, accent: 'emerald' },
      {
        label: 'Error Rate',
        value: s ? fmtPercent(s.error_rate) : '0%',
        sub: `${s?.errors ?? 0} errors`,
        icon: AlertTriangleIcon,
        accent: s && s.error_rate > 0.05 ? 'red' : 'amber',
      },
      { label: 'Avg Latency', value: s ? fmtMs(s.avg_latency_ms) : '0ms', sub: 'mean response', icon: TimerIcon, accent: 'blue' },
      { label: 'Connections', value: sys ? `${sys.healthy_connections}/${sys.total_connections}` : '0/0', sub: 'healthy / total', icon: ServerIcon, accent: 'cyan' },
      { label: 'Providers', value: sys ? fmtInt(sys.total_providers) : '0', sub: 'registered', icon: BoxesIcon, accent: 'amber' },
      { label: 'Combos', value: sys ? fmtInt(sys.total_combos) : '0', sub: 'configured', icon: LayersIcon, accent: 'fuchsia' },
    ];
  });

  function statusColor(code: number): string {
    if (code >= 500) return '#f43f5e';
    if (code >= 400) return '#f59e0b';
    if (code >= 200 && code < 300) return '#10b981';
    return '#60a5fa';
  }

  async function load() {
    loading = true;
    errorMsg = null;
    try {
      const [u, s] = await Promise.all([
        usageApi.get({ from, to, granularity: 'day' }),
        dashboardApi.stats().catch(() => null),
      ]);
      usage = u.data;
      sys = (s as SysStats) ?? null;
      await tick();
      await updateCharts();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Failed to load dashboard';
      toast.error(errorMsg);
    } finally {
      loading = false;
    }
  }

  function applyRange(r: '7d' | '30d' | 'month') {
    range = r;
    if (r === '7d') {
      from = daysAgo(6);
      to = today();
    } else if (r === '30d') {
      from = daysAgo(29);
      to = today();
    } else {
      from = startOfMonth();
      to = today();
    }
    load();
  }

  async function updateCharts() {
    if (!usage) return;
    const { default: Chart } = await import('chart.js/auto');
    const grid = 'rgba(255,255,255,0.06)';
    const tick = 'rgba(228,228,231,0.5)';

    [trafficChart, costChart, providerChart, statusChart].forEach((c) => {
      if (c) c.destroy();
    });

    if (trafficCanvas && usage.by_time.length) {
      trafficChart = new Chart(trafficCanvas, {
        type: 'bar',
        data: {
          labels: usage.by_time.map((b) => b.bucket),
          datasets: [
            { label: 'Input', data: usage.by_time.map((b) => b.input_tokens), backgroundColor: '#ec4899', stack: 't' },
            { label: 'Output', data: usage.by_time.map((b) => b.output_tokens), backgroundColor: '#a78bfa', stack: 't' },
            { label: 'Reasoning', data: usage.by_time.map((b) => b.reasoning_tokens), backgroundColor: '#60a5fa', stack: 't' },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          interaction: { mode: 'index', intersect: false },
          plugins: {
        legend: { labels: { color: tick, boxWidth: 12, font: { size: 11 } } },
        tooltip: {
          backgroundColor: '#18181b',
          titleColor: '#fafafa',
          bodyColor: '#d4d4d8',
          borderColor: '#27272a',
          borderWidth: 1,
          callbacks: { label: (ctx) => `${ctx.dataset.label}: ${Number(ctx.parsed.y).toLocaleString()} tokens` },
        },
          },
          scales: {
            x: { stacked: true, grid: { color: grid }, ticks: { color: tick, maxRotation: 0, autoSkip: true, maxTicksLimit: 12 } },
            y: { stacked: true, grid: { color: grid }, ticks: { color: tick, callback: (v) => formatTokens(Number(v)) } },
          },
        },
      });
    }

    if (costCanvas && usage.by_time.length) {
      costChart = new Chart(costCanvas, {
        type: 'line',
        data: {
          labels: usage.by_time.map((b) => b.bucket),
          datasets: [
            {
              label: 'Cost (USD)',
              data: usage.by_time.map((b) => b.cost_usd),
              borderColor: '#10b981',
              backgroundColor: 'rgba(16,185,129,0.15)',
              fill: true,
              tension: 0.35,
              pointRadius: 0,
              borderWidth: 2,
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          interaction: { mode: 'index', intersect: false },
          plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: '#18181b',
          titleColor: '#fafafa',
          bodyColor: '#d4d4d8',
          borderColor: '#27272a',
          borderWidth: 1,
          callbacks: { label: (ctx) => `Cost: $${Number(ctx.parsed.y).toFixed(2)}` },
        },
          },
          scales: {
            x: { grid: { color: grid }, ticks: { color: tick, maxRotation: 0, autoSkip: true, maxTicksLimit: 12 } },
            y: { grid: { color: grid }, ticks: { color: tick, callback: (v) => '$' + Number(v).toFixed(2) } },
          },
        },
      });
    }

    if (providerCanvas && usage.by_provider.length) {
      const palette = ['#ec4899', '#a78bfa', '#60a5fa', '#10b981', '#f59e0b', '#f472b6', '#22d3ee', '#f43f5e'];
      providerChart = new Chart(providerCanvas, {
        type: 'doughnut',
        data: {
          labels: usage.by_provider.map((p) => p.provider_name || p.provider_id || 'unknown'),
          datasets: [{ data: usage.by_provider.map((p) => p.requests), backgroundColor: palette, borderColor: '#0a0a0c', borderWidth: 2 }],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          cutout: '62%',
        plugins: {
          legend: { position: 'right', labels: { color: tick, boxWidth: 12, font: { size: 11 } } },
          tooltip: {
            backgroundColor: '#18181b',
            titleColor: '#fafafa',
            bodyColor: '#d4d4d8',
            borderColor: '#27272a',
            borderWidth: 1,
            callbacks: { label: (ctx) => `${ctx.label}: ${Number(ctx.parsed).toLocaleString()} requests` },
        },
        },
      },
      });
    }

    if (statusCanvas && usage.by_status.length) {
      const colors = usage.by_status.map((s) => statusColor(Number(s.status_code)));
      statusChart = new Chart(statusCanvas, {
        type: 'doughnut',
        data: {
          labels: usage.by_status.map((s) => String(s.status_code)),
          datasets: [{ data: usage.by_status.map((s) => s.requests), backgroundColor: colors, borderColor: '#0a0a0c', borderWidth: 2 }],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          cutout: '62%',
        plugins: {
          legend: { position: 'right', labels: { color: tick, boxWidth: 12, font: { size: 11 } } },
          tooltip: {
            backgroundColor: '#18181b',
            titleColor: '#fafafa',
            bodyColor: '#d4d4d8',
            borderColor: '#27272a',
            borderWidth: 1,
            callbacks: { label: (ctx) => `${ctx.label}: ${Number(ctx.parsed).toLocaleString()} requests` },
        },
        },
      },
      });
    }
  }

  onMount(() => {
    document.title = 'Dashboard — AxonRouter';
    load();
  });
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
    <div class="space-y-1">
      <h1 class="text-display-lg">Dashboard.</h1>
      <p class="text-body-sm text-muted-foreground">Real-time overview of traffic, cost, and system health.</p>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <Button size="sm" variant={range === '7d' ? 'default' : 'outline'} onclick={() => applyRange('7d')} class="text-body-sm rounded-sm cursor-pointer">7d</Button>
      <Button size="sm" variant={range === '30d' ? 'default' : 'outline'} onclick={() => applyRange('30d')} class="text-body-sm rounded-sm cursor-pointer">30d</Button>
      <Button size="sm" variant={range === 'month' ? 'default' : 'outline'} onclick={() => applyRange('month')} class="text-body-sm rounded-sm cursor-pointer">This month</Button>
      <Button size="sm" variant="outline" onclick={load} class="text-body-sm rounded-sm cursor-pointer gap-1.5">
        <RefreshCwIcon class="size-3.5" />Refresh
      </Button>
    </div>
  </div>

  {#if loading && !usage}
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {#each Array(8) as _}
        <div class="h-28 bg-muted animate-pulse rounded-xl"></div>
      {/each}
    </div>
    <div class="grid gap-4 lg:grid-cols-3">
      <div class="h-72 bg-muted animate-pulse rounded-xl lg:col-span-2"></div>
      <div class="h-72 bg-muted animate-pulse rounded-xl"></div>
    </div>
  {:else if errorMsg && !usage}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16">
        <p class="text-body-sm text-muted-foreground mb-4">{errorMsg}</p>
        <Button onclick={load} variant="outline" class="text-body-sm cursor-pointer">Try again</Button>
      </CardContent>
    </Card>
  {:else if usage}
    <!-- KPI cards -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {#each cards as c}
        <Card class="shadow-card relative overflow-hidden">
          <div class="absolute top-0 left-0 w-full h-0.5 {accents[c.accent].border}"></div>
          <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-1 pt-5 px-5">
            <CardTitle class="text-caption-mono text-muted-foreground uppercase">{c.label}</CardTitle>
            <c.icon class="size-4 {accents[c.accent].text}" />
          </CardHeader>
          <CardContent class="px-5 pb-5">
            <div class="text-display-sm font-semibold text-foreground tabular-nums">{c.value}</div>
            <p class="text-caption text-muted-foreground mt-1">{c.sub}</p>
          </CardContent>
        </Card>
      {/each}
    </div>

    <!-- Charts row 1 -->
    <div class="grid gap-4 lg:grid-cols-3">
      <Card class="shadow-card lg:col-span-2">
        <CardHeader class="pb-0">
          <CardTitle class="text-body-sm-strong">Token Volume Over Time</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="h-72"><canvas bind:this={trafficCanvas}></canvas></div>
        </CardContent>
      </Card>
      <Card class="shadow-card">
        <CardHeader class="pb-0">
          <CardTitle class="text-body-sm-strong">Requests by Provider</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="h-72"><canvas bind:this={providerCanvas}></canvas></div>
        </CardContent>
      </Card>
    </div>

    <!-- Charts row 2 -->
    <div class="grid gap-4 lg:grid-cols-3">
      <Card class="shadow-card lg:col-span-2">
        <CardHeader class="pb-0">
          <CardTitle class="text-body-sm-strong">Cost Over Time</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="h-72"><canvas bind:this={costCanvas}></canvas></div>
        </CardContent>
      </Card>
      <Card class="shadow-card">
        <CardHeader class="pb-0">
          <CardTitle class="text-body-sm-strong">Requests by Status</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="h-72"><canvas bind:this={statusCanvas}></canvas></div>
        </CardContent>
      </Card>
    </div>

    <!-- Tables -->
    <div class="grid gap-4 lg:grid-cols-2">
      {#if usage.by_model.length}
        <Card class="shadow-card overflow-hidden">
          <CardHeader><CardTitle class="text-body-sm-strong">Top Models</CardTitle></CardHeader>
          <CardContent class="px-0 py-0">
            <table class="w-full text-body-sm">
              <thead>
                <tr class="border-b border-border">
                  <th class="text-left px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Model</th>
                  <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Requests</th>
                  <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Tokens</th>
                  <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Cost</th>
                </tr>
              </thead>
              <tbody>
                {#each usage.by_model.slice(0, 5) as m (m.model_id)}
                  <tr class="border-b border-border/50 last:border-0 hover:bg-accent/5 transition-colors">
                    <td class="px-5 py-3 font-medium truncate max-w-[12rem]">{m.model_id}</td>
                    <td class="text-right px-5 py-3 tabular-nums">{fmtInt(m.requests)}</td>
                    <td class="text-right px-5 py-3 tabular-nums text-pink-400">{formatTokens(m.total_tokens)}</td>
                    <td class="text-right px-5 py-3 tabular-nums text-emerald-400">{money(m.cost_usd)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </CardContent>
        </Card>
      {/if}
      {#if usage.by_provider.length}
        <Card class="shadow-card overflow-hidden">
          <CardHeader><CardTitle class="text-body-sm-strong">Top Providers</CardTitle></CardHeader>
          <CardContent class="px-0 py-0">
            <table class="w-full text-body-sm">
              <thead>
                <tr class="border-b border-border">
                  <th class="text-left px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Provider</th>
                  <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Requests</th>
                  <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Tokens</th>
                  <th class="text-right px-5 py-3 text-caption-mono text-muted-foreground font-medium uppercase">Errors</th>
                </tr>
              </thead>
              <tbody>
                {#each usage.by_provider.slice(0, 5) as p (p.provider_id)}
                  <tr class="border-b border-border/50 last:border-0 hover:bg-accent/5 transition-colors">
                    <td class="px-5 py-3 font-medium truncate max-w-[12rem]">{p.provider_name || p.provider_id}</td>
                    <td class="text-right px-5 py-3 tabular-nums">{fmtInt(p.requests)}</td>
                    <td class="text-right px-5 py-3 tabular-nums text-pink-400">{formatTokens(p.total_tokens)}</td>
                    <td class="text-right px-5 py-3 tabular-nums {p.errors > 0 ? 'text-red-400' : 'text-muted-foreground'}">{fmtInt(p.errors)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </CardContent>
        </Card>
      {/if}
    </div>
  {/if}
</div>
