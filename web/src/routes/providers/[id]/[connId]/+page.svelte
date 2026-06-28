<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadConnection, selectedConnection, isLoading, error } from '$lib/stores';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  import Badge from '$lib/components/Badge.svelte';
  
  let providerId = $derived($page.params.id);
  let connectionId = $derived($page.params.connId);
  
  onMount(() => {
    loadConnection(connectionId);
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
  <title>Connection {$selectedConnection?.name || connectionId} - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Header -->
  <section class="bg-canvas-dark text-on-dark py-3xl px-3xl">
    <div class="container-max">
      <div class="flex items-center gap-lg mb-lg">
        <Button href="/providers/{providerId}" variant="ghost" size="sm">
          <span class="mono-caps-button">← BACK</span>
        </Button>
      </div>
      
      {#if $selectedConnection}
        <span class="mono-caps text-on-dark/60 mb-lg block">CONNECTION</span>
        <h1 class="display-xl mb-lg">{$selectedConnection.name}</h1>
        <div class="flex flex-wrap gap-lg">
          <Badge variant={getStatusVariant($selectedConnection.status)}>
            {$selectedConnection.status}
          </Badge>
          <Badge variant="subtle">{$selectedConnection.auth_type}</Badge>
          <Badge variant="subtle">{$selectedConnection.id}</Badge>
        </div>
      {/if}
    </div>
  </section>
  
  <!-- Content -->
  <section class="section-padding">
    <div class="container-max">
      {#if $isLoading && !$selectedConnection}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading connection...</p>
        </div>
      {:else if $error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{$error}</p>
            <Button onclick={() => loadConnection(connectionId)} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else if $selectedConnection}
        <div class="grid grid-cols-1 tablet:grid-cols-2 gap-3xl mb-section">
          <!-- Connection Details -->
          <Card>
            <h3 class="display-md mb-lg">Connection Details</h3>
            <div class="space-y-lg">
              <div>
                <span class="mono-caps text-body mb-xs block">ID</span>
                <span class="text-body-md">{$selectedConnection.id}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Name</span>
                <span class="text-body-md">{$selectedConnection.name}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Provider</span>
                <span class="text-body-md">{$selectedConnection.provider_type_id}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Auth Type</span>
                <span class="text-body-md">{$selectedConnection.auth_type}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Status</span>
                <Badge variant={getStatusVariant($selectedConnection.status)}>
                  {$selectedConnection.status}
                </Badge>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Active</span>
                <Badge variant={$selectedConnection.is_active ? 'success' : 'error'}>
                  {$selectedConnection.is_active ? 'Yes' : 'No'}
                </Badge>
              </div>
            </div>
          </Card>
          
          <!-- Status & Timing -->
          <Card>
            <h3 class="display-md mb-lg">Status & Timing</h3>
            <div class="space-y-lg">
              <div>
                <span class="mono-caps text-body mb-xs block">Cooldown Until</span>
                <span class="text-body-md">{$selectedConnection.cooldown_until ? formatCooldown($selectedConnection.cooldown_until) : 'None'}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Last Error</span>
                <span class="text-body-md text-body">{$selectedConnection.last_error || 'None'}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Last Error Code</span>
                <span class="text-body-md">{$selectedConnection.last_error_code || '-'}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Failure Count</span>
                <span class="text-body-md {$selectedConnection.failure_count > 0 ? 'text-red-600' : ''}">
                  {$selectedConnection.failure_count}
                </span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Last Success</span>
                <span class="text-body-md">{formatTimestamp($selectedConnection.last_success_at)}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Last Failure</span>
                <span class="text-body-md">{formatTimestamp($selectedConnection.last_failure_at)}</span>
              </div>
            </div>
          </Card>
        </div>
        
        <!-- Capabilities -->
        <Card class="mb-section">
          <h3 class="display-md mb-lg">Capabilities</h3>
          {#if $selectedConnection.capabilities}
            {@const capabilities = JSON.parse($selectedConnection.capabilities)}
            <div class="flex flex-wrap gap-sm">
              {#each capabilities as capability}
                <Badge variant="neutral">{capability}</Badge>
              {/each}
            </div>
          {:else}
            <p class="text-body-md text-body">No capabilities specified</p>
          {/if}
        </Card>
        
        <!-- Actions -->
        <Card class="mb-section">
          <h3 class="display-md mb-lg">Actions</h3>
          <div class="flex flex-wrap gap-md">
            <Button href="/providers/{providerId}/{connectionId}/test" variant="outline">
              <span class="mono-caps-button">TEST CONNECTION</span>
            </Button>
            <Button href="/providers/{providerId}/{connectionId}/reset" variant="outline">
              <span class="mono-caps-button">RESET STATUS</span>
            </Button>
            <Button href="/providers/{providerId}/{connectionId}/edit" variant="outline">
              <span class="mono-caps-button">EDIT</span>
            </Button>
            <Button variant="danger">
              <span class="mono-caps-button">DELETE</span>
            </Button>
          </div>
        </Card>
        
        <!-- Model Rate Limits -->
        <Card>
          <h3 class="display-md mb-lg">Model Rate Limits</h3>
          <p class="text-body-md text-body">
            Model-level rate limits will be displayed here when available.
          </p>
        </Card>
      {/if}
    </div>
  </section>
</div>
