<script lang="ts">
  import { onMount } from 'svelte';
  import { settingsApi } from '$lib/api';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  
  let settings: Record<string, string> = {};
  let isLoading = true;
  let error: string | null = null;
  let editingKey: string | null = null;
  let editingValue = '';
  
  onMount(async () => {
    await loadSettings();
  });
  
  async function loadSettings() {
    isLoading = true;
    error = null;
    
    try {
      settings = await settingsApi.list();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load settings';
    } finally {
      isLoading = false;
    }
  }
  
  function startEdit(key: string, value: string) {
    editingKey = key;
    editingValue = value;
  }
  
  function cancelEdit() {
    editingKey = null;
    editingValue = '';
  }
  
  async function saveEdit() {
    if (!editingKey) return;
    
    try {
      await settingsApi.update(editingKey, editingValue);
      settings[editingKey] = editingValue;
      editingKey = null;
      editingValue = '';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to save setting';
    }
  }
  
  // Default settings if none exist
  const defaultSettings = {
    'quota_check_interval': '30m',
    'usage_flush_interval': '5s',
    'circuit_breaker_cleanup_interval': '5m',
    'default_combo_timeout': '30000',
    'max_connections_per_provider': '1000',
    'api_rate_limit': '600',
    'log_retention_days': '30',
  };
</script>

<svelte:head>
  <title>Settings - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Header -->
  <section class="bg-canvas-dark text-on-dark py-3xl px-3xl">
    <div class="container-max">
      <span class="mono-caps text-on-dark/60 mb-lg block">SETTINGS</span>
      <h1 class="display-xl mb-lg">System Settings</h1>
      <p class="text-body-lg text-on-dark/80">
        Configure system behavior and performance settings
      </p>
    </div>
  </section>
  
  <!-- Content -->
  <section class="section-padding">
    <div class="container-max">
      {#if isLoading}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading settings...</p>
        </div>
      {:else if error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{error}</p>
            <Button on:click={loadSettings} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else}
        <!-- Settings List -->
        <div class="space-y-lg">
          {#each Object.entries({ ...defaultSettings, ...settings }) as [key, value]}
            <Card>
              <div class="flex items-center justify-between">
                <div class="flex-1">
                  <h3 class="display-md mb-xs">{key.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase())}</h3>
                  <span class="mono-caps text-body">{key}</span>
                </div>
                
                <div class="flex items-center gap-lg">
                  {#if editingKey === key}
                    <input
                      type="text"
                      class="input w-48"
                      bind:value={editingValue}
                      on:keydown={(e) => e.key === 'Enter' && saveEdit()}
                    />
                    <div class="flex gap-xs">
                      <Button on:click={saveEdit} variant="primary" size="sm">
                        <span class="mono-caps-button">SAVE</span>
                      </Button>
                      <Button on:click={cancelEdit} variant="ghost" size="sm">
                        <span class="mono-caps-button">CANCEL</span>
                      </Button>
                    </div>
                  {:else}
                    <span class="text-body-md">{value}</span>
                    <Button on:click={() => startEdit(key, value)} variant="outline" size="sm">
                      <span class="mono-caps-button">EDIT</span>
                    </Button>
                  {/if}
                </div>
              </div>
            </Card>
          {/each}
        </div>
        
        <!-- Actions -->
        <div class="mt-section">
          <Card>
            <h3 class="display-md mb-lg">Actions</h3>
            <div class="flex flex-wrap gap-md">
              <Button on:click={loadSettings} variant="outline">
                <span class="mono-caps-button">RELOAD SETTINGS</span>
              </Button>
              <Button variant="outline">
                <span class="mono-caps-button">EXPORT SETTINGS</span>
              </Button>
              <Button variant="outline">
                <span class="mono-caps-button">IMPORT SETTINGS</span>
              </Button>
            </div>
          </Card>
        </div>
        
        <!-- Info -->
        <div class="mt-section">
          <Card variant="dark">
            <h3 class="display-md mb-lg text-on-dark">About Settings</h3>
            <div class="space-y-lg text-on-dark/80">
              <p class="text-body-md">
                Settings are stored in the SQLite database and can be modified at runtime.
                Changes take effect immediately for most settings.
              </p>
              <p class="text-body-md">
                <strong>Quota Check Interval:</strong> How often to check provider quotas (default: 30 minutes)
              </p>
              <p class="text-body-md">
                <strong>Usage Flush Interval:</strong> How often to flush usage logs to database (default: 5 seconds)
              </p>
              <p class="text-body-md">
                <strong>Circuit Breaker Cleanup:</strong> How often to clean up stale circuit breaker entries (default: 5 minutes)
              </p>
              <p class="text-body-md">
                <strong>Default Combo Timeout:</strong> Default timeout for combo attempts in milliseconds (default: 30000ms)
              </p>
            </div>
          </Card>
        </div>
      {/if}
    </div>
  </section>
</div>
