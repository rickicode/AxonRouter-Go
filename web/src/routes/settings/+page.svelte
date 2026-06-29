<script lang="ts">
  import { onMount } from 'svelte';
  import { settingsApi } from '$lib/api';
  import { themeStore } from '$lib/stores/theme';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Switch } from '$lib/components/ui/switch';
  
  let settings: Record<string, string> = $state({});
  let isLoading = $state(true);
  let error: string | null = $state(null);
  let editingKey: string | null = $state(null);
  let editingValue = $state('');
  
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
  
  function formatSettingKey(key: string) {
    return key.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase());
  }
</script>

<svelte:head>
  <title>Settings - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Page header -->
  <div class="space-y-1">
    <h1 class="text-display-lg">Settings.</h1>
    <p class="text-body-sm text-muted-foreground">
      Configure system behavior and display preferences.
    </p>
  </div>
  
  <!-- Appearance Section -->
  <Card class="shadow-vercel-2 border">
    <CardHeader class="pb-3">
      <CardTitle class="text-body-md font-semibold">Appearance</CardTitle>
      <CardDescription class="text-body-sm">Customize the look and feel of the dashboard.</CardDescription>
    </CardHeader>
    <CardContent>
      <div class="flex items-center justify-between">
        <div class="space-y-0.5">
          <Label class="text-body-sm font-medium">Dark mode</Label>
          <p class="text-xs text-muted-foreground">Switch between dark and light theme.</p>
        </div>
        <Switch
          checked={$themeStore.isDark}
          onCheckedChange={() => themeStore.toggle()}
        />
      </div>
    </CardContent>
  </Card>
  
  <!-- System Settings Section -->
  <Card class="shadow-vercel-2 border">
    <CardHeader class="pb-3">
      <CardTitle class="text-body-md font-semibold">System Configuration</CardTitle>
      <CardDescription class="text-body-sm">Manage system-wide configuration keys stored in SQLite.</CardDescription>
    </CardHeader>
    <CardContent>
      {#if isLoading}
        <div class="flex flex-col gap-3">
          {#each Array(5) as _}
            <div class="h-16 bg-muted animate-pulse rounded-md"></div>
          {/each}
        </div>
      {:else if error}
        <div class="flex flex-col items-center justify-center py-8">
          <p class="text-body-sm text-muted-foreground mb-4">{error}</p>
          <Button onclick={loadSettings} variant="outline" size="sm">
            Try again
          </Button>
        </div>
      {:else}
        <div class="divide-y divide-border border rounded-md overflow-hidden bg-card">
          {#each Object.entries({ ...defaultSettings, ...settings }) as [key, value]}
            <div class="flex items-center justify-between gap-4 p-4 hover:bg-accent/10 transition-colors">
              <div class="min-w-0 flex-1 space-y-0.5">
                <h3 class="text-body-sm font-medium">{formatSettingKey(key)}</h3>
                <p class="text-caption-mono text-muted-foreground">{key}</p>
              </div>
              
              <div class="flex items-center gap-2 shrink-0">
                {#if editingKey === key}
                  <Input
                    type="text"
                    class="w-36 h-8 text-body-sm font-mono"
                    bind:value={editingValue}
                    onkeydown={(e) => e.key === 'Enter' && saveEdit()}
                  />
                  <Button onclick={saveEdit} size="sm" class="h-8 text-body-sm">
                    Save
                  </Button>
                  <Button onclick={cancelEdit} variant="ghost" size="sm" class="h-8 text-body-sm">
                    Cancel
                  </Button>
                {:else}
                  <span class="text-body-sm font-mono text-muted-foreground mr-2">{value}</span>
                  <Button onclick={() => startEdit(key, value)} variant="ghost" size="sm" class="h-8 text-body-sm">
                    Edit
                  </Button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </CardContent>
  </Card>
  
  <!-- Actions -->
  <Card class="shadow-vercel-2 border">
    <CardHeader class="pb-3">
      <CardTitle class="text-body-md font-semibold">Actions</CardTitle>
      <CardDescription class="text-body-sm">System management actions.</CardDescription>
    </CardHeader>
    <CardContent>
      <div class="flex flex-wrap gap-2">
        <Button onclick={loadSettings} variant="outline" size="sm" class="text-body-sm">
          Reload settings
        </Button>
        <Button variant="outline" size="sm" class="text-body-sm">
          Export settings
        </Button>
        <Button variant="outline" size="sm" class="text-body-sm">
          Import settings
        </Button>
      </div>
    </CardContent>
  </Card>
  
  <!-- Info card -->
  <Card class="shadow-vercel-1 border bg-accent/20">
    <CardContent class="pt-6">
      <h3 class="text-body-md font-semibold mb-3">About Settings</h3>
      <div class="space-y-2 text-body-sm text-muted-foreground">
        <p>Settings are stored in SQLite and apply immediately at runtime.</p>
        <p><strong>Quota check interval:</strong> How often to check provider quotas (default: 30 minutes)</p>
        <p><strong>Usage flush interval:</strong> How often to flush usage logs to database (default: 5 seconds)</p>
        <p><strong>Default combo timeout:</strong> Timeout for combo attempts in milliseconds (default: 30000ms)</p>
      </div>
    </CardContent>
  </Card>
</div>

