<script lang="ts">
  import { onMount } from 'svelte';
  import { settingsApi } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';

  let settings: Record<string, string> = $state({});
  let isLoading = $state(true);
  let error: string | null = $state(null);
  let editingKey: string | null = $state(null);
  let editingValue = $state('');
  let importText = $state('');
  let showImport = $state(false);

  onMount(async () => {
    document.title = 'Settings - AxonRouter';
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

  function handleExport() {
    const json = JSON.stringify(settings, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `axonrouter-settings-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function handleImport() {
    try {
      const parsed = JSON.parse(importText);
      if (typeof parsed !== 'object' || parsed === null) throw new Error('Invalid format');
      let count = 0;
      for (const [key, value] of Object.entries(parsed)) {
        if (typeof value === 'string') {
          await settingsApi.update(key, value);
          settings[key] = value;
          count++;
        }
      }
      showImport = false;
      importText = '';
      alert(`Imported ${count} settings.`);
    } catch (err) {
      alert('Import failed: ' + (err instanceof Error ? err.message : 'Invalid JSON'));
    }
  }

  const defaultSettings: Record<string, string> = {
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

  const settingDescriptions: Record<string, string> = {
    'quota_check_interval': 'How often to check provider quotas in the background',
    'usage_flush_interval': 'How often to flush buffered usage logs to SQLite',
    'circuit_breaker_cleanup_interval': 'How often to clean up expired circuit breaker states',
    'default_combo_timeout': 'Default timeout for combo routing attempts (ms)',
    'max_connections_per_provider': 'Maximum connections allowed per provider',
    'api_rate_limit': 'Maximum API requests per minute for the proxy',
    'log_retention_days': 'Number of days to keep request logs before cleanup',
  };
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Settings.</h1>
    <p class="text-body-sm text-muted-foreground">
      Configure system behavior, rate limits, and background task intervals.
    </p>
  </div>

  <Card class="shadow-vercel-2 border">
    <CardHeader class="pb-3">
      <div class="flex items-center justify-between">
        <div>
          <CardTitle class="text-body-md font-semibold">System Configuration</CardTitle>
          <CardDescription class="text-body-sm">Runtime settings stored in SQLite. Changes apply immediately.</CardDescription>
        </div>
        <Button onclick={loadSettings} variant="ghost" size="sm" class="text-body-sm">
          Reload
        </Button>
      </div>
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
          <Button onclick={loadSettings} variant="outline" size="sm">Try again</Button>
        </div>
      {:else}
        <div class="divide-y divide-border border rounded-md overflow-hidden bg-card">
          {#each Object.entries({ ...defaultSettings, ...settings }) as [key, value]}
            <div class="flex items-center justify-between gap-4 p-4 hover:bg-accent/10 transition-colors">
              <div class="min-w-0 flex-1 space-y-0.5">
                <h3 class="text-body-sm font-medium">{formatSettingKey(key)}</h3>
                <p class="text-caption-mono text-muted-foreground">{key}</p>
                {#if settingDescriptions[key]}
                  <p class="text-caption-mono text-muted-foreground/60">{settingDescriptions[key]}</p>
                {/if}
              </div>
              <div class="flex items-center gap-2 shrink-0">
                {#if editingKey === key}
                  <Input type="text" class="w-36 h-8 text-body-sm font-mono" bind:value={editingValue} onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && saveEdit()} />
                  <Button onclick={saveEdit} size="sm" class="h-8 text-body-sm">Save</Button>
                  <Button onclick={cancelEdit} variant="ghost" size="sm" class="h-8 text-body-sm">Cancel</Button>
                {:else}
                  <span class="text-body-sm font-mono text-muted-foreground mr-2">{value}</span>
                  <Button onclick={() => startEdit(key, value)} variant="ghost" size="sm" class="h-8 text-body-sm">Edit</Button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </CardContent>
  </Card>

  <Card class="shadow-vercel-2 border">
    <CardHeader class="pb-3">
      <CardTitle class="text-body-md font-semibold">Data Management</CardTitle>
      <CardDescription class="text-body-sm">Export, import, and manage settings data.</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4">
      <div class="flex flex-wrap gap-2">
        <Button onclick={handleExport} variant="outline" size="sm" class="text-body-sm">
          Export settings (JSON)
        </Button>
        <Button onclick={() => showImport = !showImport} variant="outline" size="sm" class="text-body-sm">
          {showImport ? 'Cancel import' : 'Import settings'}
        </Button>
      </div>

      {#if showImport}
        <div class="space-y-3 p-4 border border-border rounded-md bg-card">
          <Label class="text-body-sm font-medium">Paste settings JSON</Label>
          <textarea
            class="w-full h-32 bg-input border border-border rounded-md p-3 font-mono text-body-sm text-foreground placeholder:text-muted-foreground resize-y focus:outline-none focus:ring-1 focus:ring-ring"
            placeholder="Paste JSON settings object here..."
            bind:value={importText}
          ></textarea>
          <div class="flex gap-2">
            <Button onclick={handleImport} disabled={!importText.trim()} size="sm" class="text-body-sm">Import</Button>
            <Button onclick={() => { showImport = false; importText = ''; }} variant="ghost" size="sm" class="text-body-sm">Cancel</Button>
          </div>
        </div>
      {/if}
    </CardContent>
  </Card>

  <Card class="shadow-vercel-1 border bg-accent/20">
    <CardContent class="pt-6">
      <h3 class="text-body-md font-semibold mb-3">About Settings</h3>
      <div class="space-y-2 text-body-sm text-muted-foreground">
        <p>All settings are stored in SQLite with WAL mode enabled. Changes take effect immediately without restart.</p>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-3 pt-2">
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Quota check interval</p>
            <p>How often the background scheduler checks provider quotas.</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Usage flush interval</p>
            <p>Request logs are buffered in memory and flushed to SQLite at this interval.</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Circuit breaker cleanup</p>
            <p>Expired circuit breaker states are cleaned up at this interval.</p>
          </div>
          <div class="space-y-1">
            <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Default combo timeout</p>
            <p>Maximum time in milliseconds for a combo routing attempt.</p>
          </div>
        </div>
      </div>
    </CardContent>
  </Card>
</div>
