<script lang="ts">
  import { onMount } from 'svelte';
  import { loadCombos, combos, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  
  onMount(() => {
    loadCombos();
  });
</script>

<svelte:head>
  <title>Combos - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {#each Array(3) as _}
          <div class="h-56 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadCombos} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else}
    <!-- Page header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Combos.</h1>
        <p class="text-body-sm text-muted-foreground">
          {$combos.length} combos configured in the system.
        </p>
      </div>
      <Button href="/combos?action=add">
        Add combo
      </Button>
    </div>
    
    <!-- Combos Grid -->
    {#if $combos.length > 0}
      <div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {#each $combos as combo}
          <Card class="shadow-vercel-2 border transition-all hover:bg-accent/10">
            <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
              <div class="space-y-1">
                <CardTitle class="text-body-md font-medium">{combo.name}</CardTitle>
                <p class="text-caption-mono text-muted-foreground">{combo.id}</p>
              </div>
              <div class="flex gap-1">
                <Badge variant={combo.is_active ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
                  {combo.is_active ? 'Active' : 'Inactive'}
                </Badge>
                {#if combo.is_smart}
                  <Badge variant="outline" class="text-caption-mono rounded-sm">Smart</Badge>
                {/if}
              </div>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
              <div class="grid grid-cols-2 gap-3 border-t border-border/60 pt-3">
                <div>
                  <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Strategy</p>
                  <p class="text-body-sm mt-0.5">{combo.strategy}</p>
                </div>
                <div>
                  <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Timeout</p>
                  <p class="text-body-sm font-mono mt-0.5">{combo.timeout_ms}ms</p>
                </div>
                {#if combo.is_smart && combo.smart_goal}
                  <div class="col-span-2">
                    <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Smart goal</p>
                    <p class="text-body-sm font-mono mt-0.5">{combo.smart_goal}</p>
                  </div>
                {/if}
                <div class="col-span-2">
                  <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Sticky limit</p>
                  <p class="text-body-sm font-mono mt-0.5">{combo.sticky_limit}</p>
                </div>
              </div>
              
              <div class="flex gap-2 pt-2 border-t border-border mt-auto">
                <Button href="/combos/{combo.id}" variant="outline" size="sm" class="text-body-sm flex-1">
                  Edit Combo
                </Button>
                <Button href="/combos/{combo.id}/test" variant="ghost" size="sm" class="text-body-sm">
                  Test
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
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
            </svg>
          </div>
          <h3 class="text-body-md font-semibold mb-1">No combos configured</h3>
          <p class="text-body-sm text-muted-foreground mb-4">
            Create your first routing combo for fallback and load balancing.
          </p>
          <Button href="/combos?action=add">
            Add combo
          </Button>
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>

