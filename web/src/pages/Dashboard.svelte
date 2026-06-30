<script lang="ts">
  import { onMount } from 'svelte';
  import { loadDashboardStats, dashboardStats, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';

  onMount(() => {
    document.title = 'Dashboard — AxonRouter';
    loadDashboardStats();
  });

  const statCards = [
    { key: 'total_connections', label: 'Total Connections', format: (v: number) => v.toString() },
    { key: 'active_connections', label: 'Active Connections', format: (v: number) => v.toString(), color: 'text-emerald-400' },
    { key: 'total_requests_today', label: 'Requests Today', format: (v: number) => v.toString() },
    { key: 'success_rate', label: 'Success Rate', format: (v: number) => `${v}%`, color: 'text-emerald-400' },
  ] as const;
</script>

<div class="flex flex-1 flex-col gap-8 p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-8">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {#each Array(4) as _}
          <div class="h-28 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-16">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadDashboardStats} variant="outline" class="text-body-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else if $dashboardStats}
    <!-- Hero band with mesh gradient backdrop -->
    <div class="relative overflow-hidden rounded-lg border border-border bg-card p-8 md:p-10">
      <div class="gradient-mesh absolute inset-0 opacity-60 pointer-events-none"></div>
      <div class="relative space-y-3">
        <div class="inline-flex items-center gap-1.5 bg-muted/80 text-muted-foreground border border-border rounded-full px-2.5 py-0.5 text-caption-mono backdrop-blur-sm">
          <span class="size-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
          Active Router
        </div>
        <h1 class="text-display-lg">Dashboard.</h1>
        <p class="text-body-lg text-muted-foreground max-w-xl">
          Manage providers, connections, and intelligent failover routes.
        </p>
      </div>
    </div>

    <!-- Stats Grid -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      {#each statCards as stat}
        <Card class="shadow-vercel-2 border">
          <CardHeader class="pb-2 pt-5 px-5">
            <CardTitle class="text-caption-mono text-muted-foreground uppercase">{stat.label}</CardTitle>
          </CardHeader>
          <CardContent class="px-5 pb-5">
            <div class="text-display-sm font-semibold {stat.color ?? ''}">
              {stat.format($dashboardStats[stat.key])}
            </div>
          </CardContent>
        </Card>
      {/each}
    </div>

    <!-- Providers Overview -->
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-display-sm">Providers.</h2>
        <a href="/providers" class="text-body-sm text-muted-foreground hover:text-foreground transition-colors">
          View all &rarr;
        </a>
      </div>

      {#if $dashboardStats.providers?.length > 0}
        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {#each $dashboardStats.providers as provider}
            <a href="/providers/{provider.id}" class="group block">
              <Card class="shadow-vercel-2 border transition-all hover:bg-accent/10 hover:border-foreground/20 h-full">
                <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                  <div class="space-y-1">
                    <CardTitle class="text-body-md font-medium">{provider.name}</CardTitle>
                    <p class="text-caption-mono text-muted-foreground">{provider.id}</p>
                  </div>
                  <Badge variant="secondary" class="text-caption-mono rounded-sm">
                    {provider.connection_count} conns
                  </Badge>
                </CardHeader>
              </Card>
            </a>
          {/each}
        </div>
      {:else}
        <Card class="shadow-vercel-2 border">
          <CardContent class="py-12 text-center text-muted-foreground text-body-sm">
            No active providers. Add a provider to get started.
          </CardContent>
        </Card>
      {/if}
    </div>

    <!-- Quick Actions — DESIGN.md pill CTAs -->
    <div class="flex flex-wrap gap-3 pt-4 border-t border-border">
      <a href="/providers/add" class="inline-flex items-center justify-center h-10 px-5 text-button-md bg-primary text-primary-foreground rounded-pill hover:opacity-90 transition-opacity">
        Add provider
      </a>
      <a href="/logs" class="inline-flex items-center justify-center h-10 px-5 text-button-md border border-border rounded-pill hover:bg-accent/10 transition-colors">
        View logs
      </a>
      <a href="/settings" class="inline-flex items-center justify-center h-10 px-5 text-button-md text-muted-foreground hover:text-foreground transition-colors">
        Settings
      </a>
    </div>
  {/if}
</div>
