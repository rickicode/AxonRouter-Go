<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadConnection, selectedConnection, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  
  let providerId = $derived($page.params.id);
  let connectionId = $derived($page.params.connId);
  
  onMount(() => {
    loadConnection(connectionId);
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
  
  function formatTimestamp(timestamp: number | null) {
    if (!timestamp) return 'Never';
    return new Date(timestamp * 1000).toLocaleString();
  }
  
  function formatCooldown(cooldownUntil: number | null) {
    if (!cooldownUntil) return 'None';
    const now = Math.floor(Date.now() / 1000);
    if (cooldownUntil <= now) return 'Expired';
    const seconds = cooldownUntil - now;
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
    return `${Math.floor(seconds / 3600)}h`;
  }
</script>

<svelte:head>
  <title>{$selectedConnection?.name || 'Connection'} - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Back link -->
  <a href="/providers/{providerId}" class="inline-flex items-center gap-1.5 text-body-sm text-muted-foreground hover:text-foreground transition-colors w-fit">
    <svg class="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
    </svg>
    Back to provider
  </a>
  
  {#if $isLoading && !$selectedConnection}
    <div class="flex flex-col gap-6">
      <div class="h-8 w-64 bg-muted animate-pulse rounded-md"></div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div class="h-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-48 bg-muted animate-pulse rounded-md"></div>
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => loadConnection(connectionId)} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else if $selectedConnection}
    <!-- Page header -->
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        <h1 class="text-display-lg">{$selectedConnection.name}.</h1>
        <Badge variant={getStatusVariant($selectedConnection.status)} class="text-caption-mono rounded-sm">
          {$selectedConnection.status}
        </Badge>
      </div>
      <div class="flex items-center gap-2 text-caption-mono text-muted-foreground">
        <span>Auth: {$selectedConnection.auth_type}</span>
        <span>·</span>
        <span>ID: {$selectedConnection.id}</span>
      </div>
    </div>
    
    <!-- Details Grid -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <!-- Connection Details -->
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Details</CardTitle>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Provider</p>
            <p class="text-body-sm font-mono">{$selectedConnection.provider_type_id}</p>
          </div>
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Active</p>
            <Badge variant={$selectedConnection.is_active ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
              {$selectedConnection.is_active ? 'Active' : 'Disabled'}
            </Badge>
          </div>
        </CardContent>
      </Card>
      
      <!-- Status & Timing -->
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Status & Failures</CardTitle>
        </CardHeader>
        <CardContent class="grid grid-cols-2 gap-4">
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Cooldown</p>
            <p class="text-body-sm font-mono">
              {$selectedConnection.cooldown_until ? formatCooldown($selectedConnection.cooldown_until) : 'None'}
            </p>
          </div>
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Failures</p>
            <p class="text-body-sm font-semibold font-mono {$selectedConnection.failure_count > 0 ? 'text-destructive' : 'text-muted-foreground'}">
              {$selectedConnection.failure_count}
            </p>
          </div>
          <div class="space-y-1 col-span-2">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Last error</p>
            <p class="text-body-sm font-mono text-muted-foreground break-all">{$selectedConnection.last_error || 'None'}</p>
          </div>
          <div class="space-y-1 col-span-2">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Last success</p>
            <p class="text-body-sm font-mono text-muted-foreground">{formatTimestamp($selectedConnection.last_success_at)}</p>
          </div>
        </CardContent>
      </Card>
    </div>
    
    <!-- Capabilities -->
    {#if $selectedConnection.capabilities}
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Capabilities</CardTitle>
        </CardHeader>
        <CardContent>
          {@const capabilities = JSON.parse($selectedConnection.capabilities)}
          <div class="flex flex-wrap gap-1.5">
            {#each capabilities as capability}
              <Badge variant="secondary" class="text-caption-mono rounded-sm py-0.5">{capability}</Badge>
            {/each}
          </div>
        </CardContent>
      </Card>
    {/if}
    
    <!-- Actions -->
    <div class="flex flex-wrap gap-3 pt-2">
      <Button href="/providers/{providerId}/{connectionId}/test" variant="outline" class="text-body-sm">
        Test connection
      </Button>
      <Button href="/providers/{providerId}/{connectionId}/reset" variant="outline" class="text-body-sm">
        Reset status
      </Button>
      <Button href="/providers/{providerId}/{connectionId}/edit" variant="outline" class="text-body-sm">
        Edit connection
      </Button>
      <Button variant="destructive" class="text-body-sm ml-auto">
        Delete connection
      </Button>
    </div>
  {/if}
</div>

