<script lang="ts">
  import { onMount } from 'svelte';
  import { loadCombos, combos, isLoading, error } from '$lib/stores';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  import Badge from '$lib/components/Badge.svelte';
  
  onMount(() => {
    loadCombos();
  });
</script>

<svelte:head>
  <title>Combos - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Header -->
  <section class="bg-canvas-dark text-on-dark py-3xl px-3xl">
    <div class="container-max">
      <span class="mono-caps text-on-dark/60 mb-lg block">COMBOS</span>
      <h1 class="display-xl mb-lg">Routing Combos</h1>
      <p class="text-body-lg text-on-dark/80">
        Manage routing combos for fallback and load balancing
      </p>
    </div>
  </section>
  
  <!-- Content -->
  <section class="section-padding">
    <div class="container-max">
      {#if $isLoading}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading combos...</p>
        </div>
      {:else if $error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{$error}</p>
            <Button onclick={loadCombos} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else}
        <!-- Actions -->
        <div class="flex items-center justify-between mb-3xl">
          <div>
            <h2 class="display-lg">All Combos</h2>
            <p class="text-body-md text-body">{$combos.length} combos configured</p>
          </div>
          <Button href="/combos?action=add" variant="primary">
            <span class="mono-caps-button">ADD COMBO</span>
          </Button>
        </div>
        
        <!-- Combos Grid -->
        <div class="grid grid-cols-1 tablet:grid-cols-2 desktop:grid-cols-3 gap-3xl">
          {#each $combos as combo}
            <Card hover>
              <div class="flex items-start justify-between mb-lg">
                <div>
                  <h3 class="display-md mb-xs">{combo.name}</h3>
                  <span class="mono-caps text-body">{combo.id}</span>
                </div>
                <div class="flex gap-xs">
                  <Badge variant={combo.is_active ? 'success' : 'neutral'}>
                    {combo.is_active ? 'Active' : 'Inactive'}
                  </Badge>
                  {#if combo.is_smart}
                    <Badge variant="warning">Smart</Badge>
                  {/if}
                </div>
              </div>
              
              <div class="space-y-md mb-lg">
                <div>
                  <span class="mono-caps text-body mb-xs block">STRATEGY</span>
                  <span class="text-body-md">{combo.strategy}</span>
                </div>
                
                <div>
                  <span class="mono-caps text-body mb-xs block">TIMEOUT</span>
                  <span class="text-body-md">{combo.timeout_ms}ms</span>
                </div>
                
                {#if combo.is_smart && combo.smart_goal}
                  <div>
                    <span class="mono-caps text-body mb-xs block">SMART GOAL</span>
                    <span class="text-body-md">{combo.smart_goal}</span>
                  </div>
                {/if}
                
                <div>
                  <span class="mono-caps text-body mb-xs block">STICKY LIMIT</span>
                  <span class="text-body-md">{combo.sticky_limit}</span>
                </div>
              </div>
              
              <div class="flex gap-sm">
                <Button href="/combos/{combo.id}" variant="outline" size="sm">
                  <span class="mono-caps-button">EDIT</span>
                </Button>
                <Button href="/combos/{combo.id}/test" variant="ghost" size="sm">
                  <span class="mono-caps-button">TEST</span>
                </Button>
              </div>
            </Card>
          {/each}
        </div>
        
        {#if $combos.length === 0}
          <Card variant="default" padding="lg">
            <div class="text-center">
              <div class="w-16 h-16 bg-accent-mint rounded-sm flex items-center justify-center mx-auto mb-lg">
                <svg class="w-8 h-8 text-ink" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
                </svg>
              </div>
              <h3 class="display-md mb-xs">No Combos Configured</h3>
              <p class="text-body-md text-body mb-lg">
                Create your first routing combo to enable fallback and load balancing
              </p>
              <Button href="/combos?action=add" variant="primary">
                <span class="mono-caps-button">ADD COMBO</span>
              </Button>
            </div>
          </Card>
        {/if}
      {/if}
    </div>
  </section>
</div>
