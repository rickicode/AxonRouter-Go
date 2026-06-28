<script lang="ts">
  import { onMount } from 'svelte';
  import { loadProviders, providers, isLoading, error } from '$lib/stores';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  import Badge from '$lib/components/Badge.svelte';
  
  onMount(() => {
    loadProviders();
  });
  
  function getStatusVariant(status: string) {
    switch (status) {
      case 'ready':
        return 'success';
      case 'rate_limited':
      case 'quota_exhausted':
        return 'warning';
      case 'balance_empty':
      case 'auth_failed':
      case 'suspended':
        return 'error';
      default:
        return 'neutral';
    }
  }
</script>

<svelte:head>
  <title>Providers - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Header -->
  <section class="bg-canvas-dark text-on-dark py-3xl px-3xl">
    <div class="container-max">
      <span class="mono-caps text-on-dark/60 mb-lg block">PROVIDERS</span>
      <h1 class="display-xl mb-lg">AI Providers</h1>
      <p class="text-body-lg text-on-dark/80">
        Manage your AI provider connections and credentials
      </p>
    </div>
  </section>
  
  <!-- Content -->
  <section class="section-padding">
    <div class="container-max">
      {#if $isLoading}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading providers...</p>
        </div>
      {:else if $error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{$error}</p>
            <Button on:click={loadProviders} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else}
        <!-- Actions -->
        <div class="flex items-center justify-between mb-3xl">
          <div>
            <h2 class="display-lg">All Providers</h2>
            <p class="text-body-md text-body">{$providers.length} providers configured</p>
          </div>
          <Button href="/providers?action=add" variant="primary">
            <span class="mono-caps-button">ADD PROVIDER</span>
          </Button>
        </div>
        
        <!-- Providers Grid -->
        <div class="grid grid-cols-1 tablet:grid-cols-2 desktop:grid-cols-3 gap-3xl">
          {#each $providers as provider}
            <Card hover>
              <div class="flex items-start justify-between mb-lg">
                <div>
                  <h3 class="display-md mb-xs">{provider.display_name}</h3>
                  <span class="mono-caps text-body">{provider.id}</span>
                </div>
                <Badge variant={provider.is_custom ? 'neutral' : 'success'}>
                  {provider.is_custom ? 'Custom' : 'Built-in'}
                </Badge>
              </div>
              
              <div class="mb-lg">
                <span class="mono-caps text-body mb-sm block">FORMAT</span>
                <span class="text-body-md">{provider.format}</span>
              </div>
              
              <div class="mb-lg">
                <span class="mono-caps text-body mb-sm block">BASE URL</span>
                <span class="text-body-md text-body break-all">{provider.base_url}</span>
              </div>
              
              <!-- Connection Status Counts -->
              {#if provider.status_counts}
                <div class="mb-lg">
                  <span class="mono-caps text-body mb-sm block">CONNECTIONS</span>
                  <div class="flex flex-wrap gap-xs">
                    {#each Object.entries(provider.status_counts) as [status, count]}
                      {#if count > 0}
                        <Badge variant={getStatusVariant(status)} size="sm">
                          {status}: {count}
                        </Badge>
                      {/if}
                    {/each}
                  </div>
                </div>
              {/if}
              
              <div class="flex gap-sm">
                <Button href="/providers/{provider.id}" variant="outline" size="sm">
                  <span class="mono-caps-button">MANAGE</span>
                </Button>
                <Button href="/providers/{provider.id}/test" variant="ghost" size="sm">
                  <span class="mono-caps-button">TEST</span>
                </Button>
              </div>
            </Card>
          {/each}
        </div>
        
        {#if $providers.length === 0}
          <Card variant="default" padding="lg">
            <div class="text-center">
              <div class="w-16 h-16 bg-accent-mint rounded-sm flex items-center justify-center mx-auto mb-lg">
                <svg class="w-8 h-8 text-ink" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4" />
                </svg>
              </div>
              <h3 class="display-md mb-xs">No Providers Configured</h3>
              <p class="text-body-md text-body mb-lg">
                Get started by adding your first AI provider connection
              </p>
              <Button href="/providers?action=add" variant="primary">
                <span class="mono-caps-button">ADD PROVIDER</span>
              </Button>
            </div>
          </Card>
        {/if}
      {/if}
    </div>
  </section>
</div>
