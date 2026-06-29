<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadCombo, selectedCombo, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  
  let comboId = $derived($page.params.id);
  
  onMount(() => {
    loadCombo(comboId);
  });
</script>

<svelte:head>
  <title>{$selectedCombo?.name || 'Combo'} - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Back link -->
  <a href="/combos" class="inline-flex items-center gap-1.5 text-body-sm text-muted-foreground hover:text-foreground transition-colors w-fit">
    <svg class="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
    </svg>
    Back to combos
  </a>
  
  {#if $isLoading && !$selectedCombo}
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
        <Button onclick={() => loadCombo(comboId)} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else if $selectedCombo}
    <!-- Page header -->
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        <h1 class="text-display-lg">{$selectedCombo.name}.</h1>
        <Badge variant={$selectedCombo.is_active ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
          {$selectedCombo.is_active ? 'Active' : 'Inactive'}
        </Badge>
      </div>
      <div class="flex items-center gap-2 text-caption-mono text-muted-foreground">
        <span>Strategy: {$selectedCombo.strategy}</span>
        {#if $selectedCombo.is_smart}
          <span>·</span>
          <span>Smart Goal: {$selectedCombo.smart_goal}</span>
        {/if}
      </div>
    </div>
    
    <!-- Details Grid -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Details</CardTitle>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Timeout</p>
            <p class="text-body-sm font-mono">{$selectedCombo.timeout_ms}ms</p>
          </div>
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Sticky limit</p>
            <p class="text-body-sm font-mono">{$selectedCombo.sticky_limit}</p>
          </div>
        </CardContent>
      </Card>
      
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Smart settings</CardTitle>
        </CardHeader>
        <CardContent>
          {#if $selectedCombo.is_smart}
            <div class="space-y-4">
              <div class="space-y-1">
                <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Goal</p>
                <p class="text-body-sm font-mono">{$selectedCombo.smart_goal}</p>
              </div>
              <div class="space-y-1">
                <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Description</p>
                <p class="text-body-sm text-muted-foreground">
                  {#if $selectedCombo.smart_goal === 'auto'}
                    Automatically selects the best combo based on error rates and cost.
                  {:else if $selectedCombo.smart_goal === 'economy'}
                    Prefers the lowest-cost combo options.
                  {:else if $selectedCombo.smart_goal === 'balanced'}
                    Balances cost, latency, and quality.
                  {:else if $selectedCombo.smart_goal === 'premium'}
                    Prefers highest quality regardless of cost.
                  {/if}
                </p>
              </div>
            </div>
          {:else}
            <p class="text-body-sm text-muted-foreground">
              Standard combo with fixed routing steps.
            </p>
          {/if}
        </CardContent>
      </Card>
    </div>
    
    <!-- Steps -->
    <Card class="shadow-vercel-2 border">
      <CardHeader class="flex flex-row items-center justify-between pb-3">
        <CardTitle class="text-body-md font-semibold">Combo steps</CardTitle>
        <Button href="/combos/{comboId}/steps" variant="outline" size="sm" class="text-body-sm">
          Manage steps
        </Button>
      </CardHeader>
      <CardContent>
        <p class="text-body-sm text-muted-foreground">
          Define the order in which models are tried. Each step specifies a provider/model combination.
        </p>
      </CardContent>
    </Card>
    
    <!-- Actions -->
    <div class="flex flex-wrap gap-3 pt-2">
      <Button href="/combos/{comboId}/edit" variant="outline" class="text-body-sm">
        Edit combo
      </Button>
      <Button href="/combos/{comboId}/test" variant="outline" class="text-body-sm">
        Test combo
      </Button>
      <Button variant="destructive" class="text-body-sm ml-auto">
        Delete combo
      </Button>
    </div>
  {/if}
</div>

