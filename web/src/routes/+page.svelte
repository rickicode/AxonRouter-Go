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

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <!-- Loading skeleton -->
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {#each Array(4) as _}
          <div class="h-24 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {#each Array(3) as _}
          <div class="h-40 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadDashboardStats} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else if $dashboardStats}
    <!-- Page header -->
    <div class="space-y-1">
      <h1 class="text-display-lg">Dashboard.</h1>
      <p class="text-body-sm text-muted-foreground">
        Manage providers, connections, and routing system.
      </p>
    </div>
    
    <!-- Stats Grid -->
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <Card class="shadow-vercel-2 border">
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-1.5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Total Connections</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="text-display-sm font-semibold">{$dashboardStats.total_connections}</div>
        </CardContent>
      </Card>
      
      <Card class="shadow-vercel-2 border">
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-1.5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Active</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="text-display-sm font-semibold">{$dashboardStats.active_connections}</div>
        </CardContent>
      </Card>
      
      <Card class="shadow-vercel-2 border">
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-1.5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Requests Today</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="text-display-sm font-semibold">{$dashboardStats.total_requests_today}</div>
        </CardContent>
      </Card>
      
      <Card class="shadow-vercel-2 border">
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-1.5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Success Rate</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="text-display-sm font-semibold">{$dashboardStats.success_rate}%</div>
        </CardContent>
      </Card>
    </div>
    
    <!-- Providers Overview -->
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-display-md">Providers.</h2>
        <Button href="/providers" variant="ghost" size="sm" class="text-body-sm">
          View all
        </Button>
      </div>
      
      {#if $dashboardStats.providers.length > 0}
        <div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {#each $dashboardStats.providers as provider}
            <Card class="shadow-vercel-2 border transition-all hover:bg-accent/30">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                <div class="space-y-0.5">
                  <CardTitle class="text-body-md font-medium">{provider.name}</CardTitle>
                  <p class="text-caption-mono text-muted-foreground">{provider.id}</p>
                </div>
                <Badge variant="secondary" class="text-caption-mono font-medium rounded-sm">
                  {provider.connection_count} conns
                </Badge>
              </CardHeader>
              <CardContent>
                <Button href="/providers/{provider.id}" variant="outline" size="sm" class="text-body-sm w-full">
                  Manage Provider
                </Button>
              </CardContent>
            </Card>
          {/each}
        </div>
      {:else}
        <Card class="shadow-vercel-2 border">
          <CardContent class="py-10 text-center text-muted-foreground text-body-sm">
            No active providers found. Add a provider to get started.
          </CardContent>
        </Card>
      {/if}
    </div>
    
    <!-- Quick Actions -->
    <div class="space-y-3">
      <h2 class="text-display-sm">Quick actions.</h2>
      <div class="flex flex-wrap gap-3">
        <Button href="/providers?action=add">
          Add provider
        </Button>
        <Button href="/logs" variant="outline">
          View logs
        </Button>
        <Button href="/settings" variant="ghost">
          Settings
        </Button>
      </div>
    </div>
  {/if}
</div>

