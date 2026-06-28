<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadCombo, selectedCombo, isLoading, error } from '$lib/stores';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  import Badge from '$lib/components/Badge.svelte';
  
  $: comboId = $page.params.id;
  
  onMount(() => {
    loadCombo(comboId);
  });
</script>

<svelte:head>
  <title>Combo {$selectedCombo?.name || comboId} - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Header -->
  <section class="bg-canvas-dark text-on-dark py-3xl px-3xl">
    <div class="container-max">
      <div class="flex items-center gap-lg mb-lg">
        <Button href="/combos" variant="ghost" size="sm">
          <span class="mono-caps-button">← BACK</span>
        </Button>
      </div>
      
      {#if $selectedCombo}
        <span class="mono-caps text-on-dark/60 mb-lg block">COMBO</span>
        <h1 class="display-xl mb-lg">{$selectedCombo.name}</h1>
        <div class="flex flex-wrap gap-lg">
          <Badge variant={$selectedCombo.is_active ? 'success' : 'neutral'}>
            {$selectedCombo.is_active ? 'Active' : 'Inactive'}
          </Badge>
          <Badge variant="subtle">{$selectedCombo.strategy}</Badge>
          {#if $selectedCombo.is_smart}
            <Badge variant="warning">Smart: {$selectedCombo.smart_goal}</Badge>
          {/if}
        </div>
      {/if}
    </div>
  </section>
  
  <!-- Content -->
  <section class="section-padding">
    <div class="container-max">
      {#if $isLoading && !$selectedCombo}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading combo...</p>
        </div>
      {:else if $error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{$error}</p>
            <Button on:click={() => loadCombo(comboId)} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else if $selectedCombo}
        <div class="grid grid-cols-1 tablet:grid-cols-2 gap-3xl mb-section">
          <!-- Combo Details -->
          <Card>
            <h3 class="display-md mb-lg">Combo Details</h3>
            <div class="space-y-lg">
              <div>
                <span class="mono-caps text-body mb-xs block">ID</span>
                <span class="text-body-md">{$selectedCombo.id}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Name</span>
                <span class="text-body-md">{$selectedCombo.name}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Strategy</span>
                <span class="text-body-md">{$selectedCombo.strategy}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Timeout</span>
                <span class="text-body-md">{$selectedCombo.timeout_ms}ms</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Sticky Limit</span>
                <span class="text-body-md">{$selectedCombo.sticky_limit}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Active</span>
                <Badge variant={$selectedCombo.is_active ? 'success' : 'error'}>
                  {$selectedCombo.is_active ? 'Yes' : 'No'}
                </Badge>
              </div>
            </div>
          </Card>
          
          <!-- Smart Combo Settings -->
          <Card>
            <h3 class="display-md mb-lg">Smart Combo Settings</h3>
            {#if $selectedCombo.is_smart}
              <div class="space-y-lg">
                <div>
                  <span class="mono-caps text-body mb-xs block">Smart Goal</span>
                  <span class="text-body-md">{$selectedCombo.smart_goal}</span>
                </div>
                <div>
                  <span class="mono-caps text-body mb-xs block">Description</span>
                  <p class="text-body-md text-body">
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
              <p class="text-body-md text-body">
                This is a standard combo with fixed routing steps.
              </p>
            {/if}
          </Card>
        </div>
        
        <!-- Combo Steps -->
        <Card class="mb-section">
          <div class="flex items-center justify-between mb-lg">
            <h3 class="display-md">Combo Steps</h3>
            <Button href="/combos/{comboId}/steps" variant="outline" size="sm">
              <span class="mono-caps-button">MANAGE STEPS</span>
            </Button>
          </div>
          
          <p class="text-body-md text-body">
            Combo steps define the order in which models are tried. Each step specifies a provider/model combination.
          </p>
          
          <div class="mt-lg">
            <Button href="/combos/{comboId}/steps" variant="primary" size="sm">
              <span class="mono-caps-button">VIEW STEPS</span>
            </Button>
          </div>
        </Card>
        
        <!-- Actions -->
        <Card class="mb-section">
          <h3 class="display-md mb-lg">Actions</h3>
          <div class="flex flex-wrap gap-md">
            <Button href="/combos/{comboId}/edit" variant="outline">
              <span class="mono-caps-button">EDIT COMBO</span>
            </Button>
            <Button href="/combos/{comboId}/test" variant="outline">
              <span class="mono-caps-button">TEST COMBO</span>
            </Button>
            <Button variant="danger">
              <span class="mono-caps-button">DELETE COMBO</span>
            </Button>
          </div>
        </Card>
        
        <!-- Usage Statistics -->
        <Card>
          <h3 class="display-md mb-lg">Usage Statistics</h3>
          <p class="text-body-md text-body">
            Usage statistics for this combo will be displayed here.
          </p>
        </Card>
      {/if}
    </div>
  </section>
</div>
