<script lang="ts">
  import { onMount } from 'svelte';
  import { loadDashboardStats, dashboardStats, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  
  onMount(() => {
    loadDashboardStats();
  });
</script>

<svelte:head>
  <title>Dashboard - AxonRouter</title>
</svelte:head>

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
        <Button onclick={loadDashboardStats} variant="outline">Try again</Button>
      </CardContent>
    </Card>
  {:else if $dashboardStats}
    <!-- Page header -->
    <div class="space-y-2">
      <div class="inline-flex items-center gap-1.5 bg-muted text-muted-foreground border border-border rounded-full px-2.5 py-0.5 text-caption-mono">
        <span class="size-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
        Active Router
      </div>
      <h1 class="text-display-lg">Dashboard.</h1>
      <p class="text-body-sm text-muted-foreground">
        Manage providers, connections, and intelligent failover routes.
      </p>
    </div>

    <!-- Stats Grid -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Total Connections</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold">{$dashboardStats.total_connections}</div>
        </CardContent>
      </Card>

      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Active Connections</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-emerald-500">{$dashboardStats.active_connections}</div>
        </CardContent>
      </Card>

      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Requests Today</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold">{$dashboardStats.total_requests_today}</div>
        </CardContent>
      </Card>

      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Success Rate</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-emerald-500">{$dashboardStats.success_rate}%</div>
        </CardContent>
      </Card>
    </div>

    <!-- Providers Overview -->
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-display-sm">Providers.</h2>
        <Button href="/providers" variant="ghost" size="sm" class="text-body-sm text-muted-foreground hover:text-foreground">
          View all &rarr;
        </Button>
      </div>

      {#if $dashboardStats.providers.length > 0}
        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {#each $dashboardStats.providers as provider}
            <Card class="shadow-vercel-2 border transition-all hover:bg-accent/10">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                <div class="space-y-1">
                  <CardTitle class="text-body-md font-medium">{provider.name}</CardTitle>
                  <p class="text-caption-mono text-muted-foreground">{provider.id}</p>
                </div>
                <Badge variant="secondary" class="text-caption-mono rounded-sm">
                  {provider.connection_count} conns
                </Badge>
              </CardHeader>
              <CardContent>
                <Button href="/providers/{provider.id}" variant="outline" size="sm" class="text-body-sm w-full">
                  Manage
                </Button>
              </CardContent>
            </Card>
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

    <!-- Quick Actions -->
    <div class="flex flex-wrap gap-3 pt-4 border-t border-border">
      <Button href="/providers/add" class="h-9 px-4 text-body-sm">Add provider</Button>
      <Button href="/logs" variant="outline" class="h-9 px-4 text-body-sm">View logs</Button>
      <Button href="/settings" variant="ghost" class="h-9 px-4 text-body-sm text-muted-foreground">Settings</Button>
    </div>
  {/if}
</div>
