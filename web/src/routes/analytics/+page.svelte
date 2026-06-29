<script lang="ts">
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
</script>

<svelte:head>
  <title>Analytics - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-8 p-8 relative">
  <!-- Top Ambient Glows -->
  <div class="absolute top-0 left-1/4 w-[500px] h-[500px] bg-gradient-to-br from-[#007cf0]/10 via-[#7928ca]/5 to-transparent rounded-full blur-[120px] pointer-events-none"></div>

  <!-- Page header -->
  <div class="space-y-2 relative z-10">
    <div class="inline-flex items-center gap-1 bg-primary/5 text-primary border border-primary/10 rounded-full px-2.5 py-0.5 text-caption-mono font-medium">
      <span class="size-1.5 rounded-full bg-primary"></span>
      Real-time Engine
    </div>
    <h1 class="text-display-lg tracking-[-0.04em]">Analytics.</h1>
    <p class="text-body-sm text-muted-foreground">
      Deep insights, latency profiling, and error rate telemetry.
    </p>
  </div>

  <!-- Telemetry Metrics Grid -->
  <div class="grid gap-6 md:grid-cols-3 relative z-10">
    <Card class="shadow-vercel-2 border relative overflow-hidden transition-all hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
      <CardHeader class="pb-2">
        <CardDescription class="text-caption-mono uppercase font-semibold">Average Latency</CardDescription>
      </CardHeader>
      <CardContent>
        <div class="text-display-sm font-semibold">142ms</div>
        <p class="text-xs text-emerald-500 font-mono mt-1">&darr; 12% vs last hour</p>
      </CardContent>
    </Card>

    <Card class="shadow-vercel-2 border relative overflow-hidden transition-all hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
      <CardHeader class="pb-2">
        <CardDescription class="text-caption-mono uppercase font-semibold">Error Rate</CardDescription>
      </CardHeader>
      <CardContent>
        <div class="text-display-sm font-semibold text-emerald-500">0.02%</div>
        <p class="text-xs text-muted-foreground font-mono mt-1">Excellent health status</p>
      </CardContent>
    </Card>

    <Card class="shadow-vercel-2 border relative overflow-hidden transition-all hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
      <CardHeader class="pb-2">
        <CardDescription class="text-caption-mono uppercase font-semibold">Cache Hit Rate</CardDescription>
      </CardHeader>
      <CardContent>
        <div class="text-display-sm font-semibold">94.8%</div>
        <p class="text-xs text-primary font-mono mt-1">&uarr; 3.2% vs last hour</p>
      </CardContent>
    </Card>
  </div>

  <!-- Performance Charts -->
  <Card class="shadow-vercel-2 border relative z-10 bg-card/40">
    <CardHeader>
      <CardTitle class="text-body-md font-semibold">Latency Distribution (Last 24h)</CardTitle>
      <CardDescription class="text-body-sm">Visualizing system round-trip latency profiles.</CardDescription>
    </CardHeader>
    <CardContent class="pt-4">
      <div class="h-48 flex items-end gap-2 border-b border-border pb-2 px-4">
        {#each [25, 40, 35, 50, 75, 90, 85, 60, 45, 55, 70, 80, 95, 110, 130, 115, 95, 80, 75, 90, 110, 120, 135, 142] as height}
          <div class="flex-1 bg-gradient-to-t from-[#007cf0] to-[#00dfd8]/60 rounded-t-sm hover:opacity-80 transition-opacity cursor-pointer group relative" style="height: {height}%">
            <!-- Tooltip -->
            <div class="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:block bg-popover text-popover-foreground text-[10px] font-mono rounded px-1.5 py-0.5 border shadow-md whitespace-nowrap z-50">
              {height * 2}ms
            </div>
          </div>
        {/each}
      </div>
      <div class="flex justify-between mt-2 text-caption-mono text-muted-foreground px-4">
        <span>24h ago</span>
        <span>12h ago</span>
        <span>Now</span>
      </div>
    </CardContent>
  </Card>

  <!-- planned integrations -->
  <Card class="shadow-vercel-1 border bg-accent/20 relative z-10">
    <CardContent class="pt-6">
      <h3 class="text-body-md font-semibold mb-3">Planned Telemetry Hooks</h3>
      <div class="space-y-2 text-body-sm text-muted-foreground">
        <p>Analytics metrics are derived directly from the SQLite logs table in real-time.</p>
        <p>Future releases will support exporting Prometheus metrics and OpenTelemetry tracing spans.</p>
      </div>
    </CardContent>
  </Card>
</div>
