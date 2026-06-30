<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { settingsApi } from '$lib/api';
  import { toast } from 'svelte-sonner';

  let settings: Record<string, string> = $state({});
  let loading = $state(true);
  let error = $state<string | null>(null);
  let editingKey = $state<string | null>(null);
  let editingValue = $state('');
  let showImport = $state(false);
  let importText = $state('');

  const defaultSettings: Record<string, string> = {
    'quota_check_interval': '60s',
    'usage_flush_interval': '30s',
    'circuit_breaker_cleanup': '5m',
    'default_combo_timeout': '30000',
    'max_retries': '3',
    'log_retention_days': '30',
  };

  const settingDescriptions: Record<string, string> = {
    'quota_check_interval': 'How often to check provider quotas',
    'usage_flush_interval': 'How often to flush usage data to disk',
    'circuit_breaker_cleanup': 'How often to clean up expired circuit breakers',
    'default_combo_timeout': 'Default timeout for combo routing (ms)',
    'max_retries': 'Maximum retry attempts per request',
    'log_retention_days': 'Days to keep request logs',
  };

  onMount(() => {
    document.title = 'Settings — AxonRouter';
    loadSettings();
  });

  async function loadSettings() {
    loading = true;
    error = null;
    try {
      const response = await settingsApi.list() as Record<string, string> | { data: Record<string, string> };
      settings = 'data' in response ? response.data : response;
    } catch (err) {
      error = err instanceof Error ? err.message : 'Failed to load settings';
    } finally {
      loading = false;
    }
  }

  function formatSettingKey(key: string): string {
    return key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
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
      toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleExport() {
    const blob = new Blob([JSON.stringify(settings, null, 2)], { type: 'application/json' });
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
      if (typeof parsed !== 'object' || parsed === null) throw new Error('Invalid JSON');
      for (const [key, value] of Object.entries(parsed)) {
        if (typeof value === 'string') {
          await settingsApi.update(key, value);
          settings[key] = value;
        }
      }
      showImport = false;
      importText = '';
    } catch (err) {
      toast.error('Import failed: ' + (err instanceof Error ? err.message : 'Invalid JSON'));
    }
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Settings.</h1>
    <p class="text-body-sm text-muted-foreground">
      Configure system behavior, rate limits, and background task intervals.
    </p>
  </div>

  <Card class="shadow-card">
    <CardHeader class="pb-3">
      <div class="flex items-center justify-between">
        <div>
          <CardTitle class="text-body-md-strong">System Configuration</CardTitle>
          <CardDescription class="text-body-sm">Runtime settings stored in SQLite. Changes apply immediately.</CardDescription>
        </div>
        <Button onclick={loadSettings} variant="ghost" size="sm" class="text-body-sm rounded-sm">
          Reload
        </Button>
      </div>
    </CardHeader>
    <CardContent>
      {#if loading}
        <div class="flex flex-col gap-3">
          {#each Array(5) as _}
            <div class="h-16 bg-muted animate-pulse rounded-md"></div>
          {/each}
        </div>
      {:else if error}
        <div class="flex flex-col items-center justify-center py-8">
          <p class="text-body-sm text-muted-foreground mb-4">{error}</p>
          <Button onclick={loadSettings} variant="outline" size="sm" class="text-body-sm rounded-sm">Try again</Button>
        </div>
      {:else}
        <div class="divide-y divide-border border rounded-md overflow-hidden bg-card">
          {#each Object.entries({ ...defaultSettings, ...settings }) as [key, value]}
            <div class="flex items-center justify-between gap-4 p-4 hover:bg-accent/10 transition-colors">
              <div class="min-w-0 flex-1 space-y-0.5">
                <h3 class="text-body-sm-strong">{formatSettingKey(key)}</h3>
                <p class="text-caption-mono text-muted-foreground">{key}</p>
                {#if settingDescriptions[key]}
                  <p class="text-caption text-muted-foreground/60">{settingDescriptions[key]}</p>
                {/if}
              </div>
              <div class="flex items-center gap-2 shrink-0">
                {#if editingKey === key}
                  <Input type="text" class="w-36 h-8 text-body-sm font-mono" bind:value={editingValue} onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && saveEdit()} />
                  <Button onclick={saveEdit} size="sm" class="h-8 text-body-sm rounded-sm">Save</Button>
                  <Button onclick={cancelEdit} variant="ghost" size="sm" class="h-8 text-body-sm">Cancel</Button>
                {:else}
                  <span class="text-code font-mono text-muted-foreground mr-2">{value}</span>
                  <Button onclick={() => startEdit(key, value)} variant="ghost" size="sm" class="h-8 text-body-sm rounded-sm">Edit</Button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </CardContent>
  </Card>

  <Card class="shadow-card">
    <CardHeader class="pb-3">
      <CardTitle class="text-body-md-strong">Data Management</CardTitle>
      <CardDescription class="text-body-sm">Export, import, and manage settings data.</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4">
      <div class="flex flex-wrap gap-2">
        <Button onclick={handleExport} variant="outline" size="sm" class="text-body-sm rounded-sm">
          Export settings (JSON)
        </Button>
        <Button onclick={() => showImport = !showImport} variant="outline" size="sm" class="text-body-sm rounded-sm">
          {showImport ? 'Cancel import' : 'Import settings'}
        </Button>
      </div>

      {#if showImport}
        <div class="space-y-3 p-4 shadow-card rounded-md bg-card">
          <Label class="text-body-sm-strong">Paste settings JSON</Label>
          <textarea
            class="w-full h-32 bg-input rounded-md p-3 text-code font-mono text-foreground placeholder:text-muted-foreground resize-y focus:outline-none focus:ring-1 focus:ring-ring"
            placeholder="Paste JSON settings object here..."
            bind:value={importText}
          ></textarea>
          <div class="flex gap-2">
            <Button onclick={handleImport} disabled={!importText.trim()} size="sm" class="text-body-sm rounded-sm">Import</Button>
            <Button onclick={() => { showImport = false; importText = ''; }} variant="ghost" size="sm" class="text-body-sm">Cancel</Button>
          </div>
        </div>
      {/if}
    </CardContent>
  </Card>

  <Card class="shadow-card bg-accent/20">
    <CardContent class="pt-6">
      <h3 class="text-body-md-strong mb-3">About Settings.</h3>
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
