<script lang="ts">
  import { onMount } from 'svelte';
  import { loadDashboardStats, dashboardStats, isLoading, error } from '$lib/stores';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  import Badge from '$lib/components/Badge.svelte';
  
  onMount(() => {
    loadDashboardStats();
  });
</script>

<svelte:head>
  <title>Dashboard - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Hero Section -->
  <section class="bg-canvas-dark text-on-dark section-padding">
    <div class="container-max">
      <div class="max-w-3xl">
        <span class="mono-caps text-on-dark/60 mb-lg block">DASHBOARD</span>
        <h1 class="display-xxl mb-lg">
          AxonRouter-Go
        </h1>
        <p class="text-body-lg text-on-dark/80 mb-3xl">
          Universal API proxy for coding agents. Manage providers, connections, and routing from a single dashboard.
        </p>
        <div class="flex gap-md">
          <Button href="/providers" variant="secondary">
            <span class="mono-caps-button">VIEW PROVIDERS</span>
          </Button>
          <Button href="/logs" variant="ghost">
            <span class="mono-caps-button">VIEW LOGS</span>
          </Button>
        </div>
      </div>
    </div>
  </section>
  
  <!-- Stats Section -->
  <section class="section-padding">
    <div class="container-max">
      {#if $isLoading}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading dashboard...</p>
        </div>
      {:else if $error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{$error}</p>
            <Button on:click={loadDashboardStats} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else if $dashboardStats}
        <!-- Stats Grid -->
        <div class="grid grid-cols-1 tablet:grid-cols-2 desktop:grid-cols-4 gap-3xl mb-section">
          <Card variant="tinted" padding="lg">
            <span class="mono-caps text-body mb-sm block">TOTAL CONNECTIONS</span>
            <span class="display-xl text-ink">{$dashboardStats.total_connections}</span>
          </Card>
          
          <Card variant="tinted" padding="lg">
            <span class="mono-caps text-body mb-sm block">ACTIVE CONNECTIONS</span>
            <span class="display-xl text-ink">{$dashboardStats.active_connections}</span>
          </Card>
          
          <Card variant="tinted" padding="lg">
            <span class="mono-caps text-body mb-sm block">REQUESTS TODAY</span>
            <span class="display-xl text-ink">{$dashboardStats.total_requests_today}</span>
          </Card>
          
          <Card variant="tinted" padding="lg">
            <span class="mono-caps text-body mb-sm block">SUCCESS RATE</span>
            <span class="display-xl text-ink">{$dashboardStats.success_rate}%</span>
          </Card>
        </div>
        
        <!-- Providers Overview -->
        <div class="mb-section">
          <div class="flex items-center justify-between mb-3xl">
            <h2 class="display-lg">Providers</h2>
            <Button href="/providers" variant="outline">
              <span class="mono-caps-button">VIEW ALL</span>
            </Button>
          </div>
          
          <div class="grid grid-cols-1 tablet:grid-cols-2 desktop:grid-cols-3 gap-3xl">
            {#each $dashboardStats.providers as provider}
              <Card hover>
                <div class="flex items-start justify-between mb-lg">
                  <div>
                    <h3 class="display-md mb-xs">{provider.name}</h3>
                    <span class="mono-caps text-body">{provider.id}</span>
                  </div>
                  <Badge variant="success">
                    {provider.connection_count} connections
                  </Badge>
                </div>
                
                <div class="flex gap-sm">
                  <Button href="/providers/{provider.id}" variant="outline" size="sm">
                    <span class="mono-caps-button">MANAGE</span>
                  </Button>
                </div>
              </Card>
            {/each}
          </div>
        </div>
        
        <!-- Quick Actions -->
        <div>
          <h2 class="display-lg mb-3xl">Quick Actions</h2>
          
          <div class="grid grid-cols-1 tablet:grid-cols-3 gap-3xl">
            <Card hover>
              <div class="text-center">
                <div class="w-12 h-12 bg-accent-mint rounded-sm flex items-center justify-center mx-auto mb-lg">
                  <svg class="w-6 h-6 text-ink" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                  </svg>
                </div>
                <h3 class="display-md mb-xs">Add Provider</h3>
                <p class="text-body-md text-body mb-lg">Connect a new AI provider to the proxy</p>
                <Button href="/providers?action=add" variant="primary" size="sm">
                  <span class="mono-caps-button">ADD PROVIDER</span>
                </Button>
              </div>
            </Card>
            
            <Card hover>
              <div class="text-center">
                <div class="w-12 h-12 bg-accent-mint rounded-sm flex items-center justify-center mx-auto mb-lg">
                  <svg class="w-6 h-6 text-ink" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
                  </svg>
                </div>
                <h3 class="display-md mb-xs">View Logs</h3>
                <p class="text-body-md text-body mb-lg">Monitor request logs and performance</p>
                <Button href="/logs" variant="primary" size="sm">
                  <span class="mono-caps-button">VIEW LOGS</span>
                </Button>
              </div>
            </Card>
            
            <Card hover>
              <div class="text-center">
                <div class="w-12 h-12 bg-accent-mint rounded-sm flex items-center justify-center mx-auto mb-lg">
                  <svg class="w-6 h-6 text-ink" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                  </svg>
                </div>
                <h3 class="display-md mb-xs">Settings</h3>
                <p class="text-body-md text-body mb-lg">Configure system settings and preferences</p>
                <Button href="/settings" variant="primary" size="sm">
                  <span class="mono-caps-button">SETTINGS</span>
                </Button>
              </div>
            </Card>
          </div>
        </div>
      {/if}
    </div>
  </section>
</div>
