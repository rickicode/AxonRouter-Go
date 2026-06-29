<script lang="ts">
  import { onMount } from 'svelte';
  import { loadProviders, providers, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  
  onMount(() => {
    loadProviders();
  });
  
  function getStatusVariant(status: string): 'default' | 'secondary' | 'destructive' {
    switch (status) {
      case 'ready':
        return 'default';
      case 'rate_limited':
      case 'quota_exhausted':
        return 'secondary';
      case 'balance_empty':
      case 'auth_failed':
      case 'suspended':
        return 'destructive';
      default:
        return 'secondary';
    }
  }
</script>

<svelte:head>
  <title>Providers - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {#each Array(6) as _}
          <div class="h-48 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadProviders} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else}
    <!-- Page header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Providers.</h1>
        <p class="text-body-sm text-muted-foreground">
          {$providers.length} providers configured in the system.
        </p>
      </div>
      <Button href="/providers?action=add">
        Add provider
      </Button>
    </div>
    
    <!-- Providers Grid -->
    {#if $providers.length > 0}
      <div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {#each $providers as provider}
          <Card class="shadow-vercel-2 border transition-all hover:bg-accent/10">
            <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
              <div class="space-y-1">
                <CardTitle class="text-body-md font-medium">{provider.display_name}</CardTitle>
                <p class="text-caption-mono text-muted-foreground">{provider.id}</p>
              </div>
              <Badge variant={provider.is_custom ? 'secondary' : 'default'} class="text-caption-mono rounded-sm">
                {provider.is_custom ? 'Custom' : 'Built-in'}
              </Badge>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
              <div class="grid gap-2">
                <div>
                  <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Format</p>
                  <p class="text-body-sm font-mono text-foreground mt-0.5">{provider.format}</p>
                </div>
                <div>
                  <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Base URL</p>
                  <p class="text-body-sm text-muted-foreground font-mono truncate mt-0.5" title={provider.base_url}>{provider.base_url}</p>
                </div>
              </div>
              
              <!-- Connection Status -->
              {#if provider.status_counts}
                <div class="space-y-1.5">
                  <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Connections</p>
                  <div class="flex flex-wrap gap-1.5">
                    {#each Object.entries(provider.status_counts) as [status, count]}
                      {#if count > 0}
                        <Badge variant={getStatusVariant(status)} class="text-caption-mono rounded-sm py-0.5">
                          {status}: {count}
                        </Badge>
                      {/if}
                    {/each}
                  </div>
                </div>
              {/if}
              
              <div class="flex gap-2 pt-2 border-t border-border mt-auto">
                <Button href="/providers/{provider.id}" variant="outline" size="sm" class="text-body-sm flex-1">
                  Manage
                </Button>
                <Button href="/providers/{provider.id}/test" variant="ghost" size="sm" class="text-body-sm">
                  Test All
                </Button>
              </div>
            </CardContent>
          </Card>
        {/each}
      </div>
    {:else}
      <!-- Empty state -->
      <Card class="shadow-vercel-2 border">
        <CardContent class="flex flex-col items-center justify-center py-16">
          <div class="size-12 bg-muted rounded-md flex items-center justify-center mb-4">
            <svg class="size-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4" />
            </svg>
          </div>
          <h3 class="text-body-md font-semibold mb-1">No providers configured</h3>
          <p class="text-body-sm text-muted-foreground mb-4">
            Add your first AI provider to get started.
          </p>
          <Button href="/providers?action=add">
            Add provider
          </Button>
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>

