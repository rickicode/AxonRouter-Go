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

<div class="flex flex-1 flex-col gap-8 p-8 relative">
  <!-- Top Ambient Glows -->
  <div class="absolute top-0 left-1/4 w-[500px] h-[500px] bg-gradient-to-br from-[#007cf0]/10 via-[#7928ca]/5 to-transparent rounded-full blur-[120px] pointer-events-none"></div>
  <div class="absolute top-20 right-1/4 w-[400px] h-[400px] bg-gradient-to-br from-[#ff4d4d]/10 via-[#f9cb28]/5 to-transparent rounded-full blur-[100px] pointer-events-none"></div>

  {#if $isLoading}
    <!-- Loading skeleton -->
    <div class="flex flex-col gap-8 relative z-10">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        {#each Array(4) as _}
          <div class="h-28 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-8">
        {#each Array(3) as _}
          <div class="h-40 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border relative z-10">
      <CardContent class="flex flex-col items-center justify-center py-16">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadDashboardStats} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else if $dashboardStats}
    <!-- Page header -->
    <div class="space-y-2 relative z-10">
      <div class="inline-flex items-center gap-1 bg-primary/5 text-primary border border-primary/10 rounded-full px-2.5 py-0.5 text-caption-mono font-medium">
        <span class="size-1.5 rounded-full bg-primary animate-pulse"></span>
        Active Router
      </div>
      <h1 class="text-display-lg tracking-[-0.04em]">Dashboard.</h1>
      <p class="text-body-sm text-muted-foreground">
        Manage providers, connections, and intelligent failover routes.
      </p>
    </div>
    
    <!-- Stats Grid -->
    <div class="grid gap-6 md:grid-cols-2 lg:grid-cols-4 relative z-10">
      <!-- Connection Stat -->
      <Card class="shadow-vercel-2 border relative overflow-hidden transition-all duration-300 hover:translate-y-[-2px] hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
        <div class="bg-gradient-to-r from-[#007cf0] to-[#00dfd8] h-[2px] w-full absolute top-0 left-0"></div>
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Total Connections</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold">{$dashboardStats.total_connections}</div>
        </CardContent>
      </Card>
      
      <!-- Active Stat -->
      <Card class="shadow-vercel-2 border relative overflow-hidden transition-all duration-300 hover:translate-y-[-2px] hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
        <div class="bg-gradient-to-r from-[#7928ca] to-[#ff0080] h-[2px] w-full absolute top-0 left-0"></div>
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Active Connections</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-[#ff0080]">{$dashboardStats.active_connections}</div>
        </CardContent>
      </Card>
      
      <!-- Request Stat -->
      <Card class="shadow-vercel-2 border relative overflow-hidden transition-all duration-300 hover:translate-y-[-2px] hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
        <div class="bg-gradient-to-r from-[#ff4d4d] to-[#f9cb28] h-[2px] w-full absolute top-0 left-0"></div>
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Requests Today</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold">{$dashboardStats.total_requests_today}</div>
        </CardContent>
      </Card>
      
      <!-- Success Rate Stat -->
      <Card class="shadow-vercel-2 border relative overflow-hidden transition-all duration-300 hover:translate-y-[-2px] hover:shadow-vercel-3 bg-card/60 backdrop-blur-sm">
        <div class="bg-gradient-to-r from-[#10b981] to-[#34d399] h-[2px] w-full absolute top-0 left-0"></div>
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2 pt-5 px-5">
          <CardTitle class="text-caption-mono text-muted-foreground uppercase">Success Rate</CardTitle>
        </CardHeader>
        <CardContent class="px-5 pb-5">
          <div class="text-display-sm font-semibold text-[#10b981]">{$dashboardStats.success_rate}%</div>
        </CardContent>
      </Card>
    </div>
    
    <!-- Providers Overview -->
    <div class="space-y-5 relative z-10">
      <div class="flex items-center justify-between">
        <h2 class="text-display-md tracking-tight">Providers.</h2>
        <Button href="/providers" variant="ghost" size="sm" class="text-body-sm text-muted-foreground hover:text-foreground">
          View all providers &rarr;
        </Button>
      </div>
      
      {#if $dashboardStats.providers.length > 0}
        <div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {#each $dashboardStats.providers as provider}
            <Card class="shadow-vercel-2 border transition-all duration-300 hover:translate-y-[-2px] hover:shadow-vercel-3 bg-card/40 hover:bg-card/90">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                <div class="space-y-1">
                  <CardTitle class="text-body-md font-semibold">{provider.name}</CardTitle>
                  <p class="text-caption-mono text-muted-foreground">{provider.id}</p>
                </div>
                <Badge variant="secondary" class="text-caption-mono font-medium rounded-sm border bg-background/50">
                  {provider.connection_count} conns
                </Badge>
              </CardHeader>
              <CardContent>
                <Button href="/providers/{provider.id}" variant="outline" size="sm" class="text-body-sm w-full font-medium h-9 border-border/80">
                  Manage Provider
                </Button>
              </CardContent>
            </Card>
          {/each}
        </div>
      {:else}
        <Card class="shadow-vercel-2 border bg-card/40">
          <CardContent class="py-12 text-center text-muted-foreground text-body-sm">
            No active providers found. Add a provider to get started.
          </CardContent>
        </Card>
      {/if}
    </div>
    
    <!-- Quick Actions -->
    <div class="space-y-4 relative z-10 pt-4 border-t border-border/60">
      <h2 class="text-display-sm tracking-tight">Quick actions.</h2>
      <div class="flex flex-wrap gap-3">
        <Button href="/providers?action=add" class="h-9 px-4 text-body-sm">
          Add provider
        </Button>
        <Button href="/logs" variant="outline" class="h-9 px-4 text-body-sm border-border/80">
          View logs
        </Button>
        <Button href="/settings" variant="ghost" class="h-9 px-4 text-body-sm hover:bg-accent/50 text-muted-foreground hover:text-foreground">
          Settings
        </Button>
      </div>
    </div>
  {/if}
</div>


