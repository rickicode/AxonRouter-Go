<script lang="ts">
import { onMount } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Textarea } from '$lib/components/ui/textarea';
import * as Tabs from '$lib/components/ui/tabs';
import { settingsApi } from '$lib/api';
import { toast } from 'svelte-sonner';
import ChangePasswordCard from '$lib/components/ChangePasswordCard.svelte';
import HttpsSettings from '$lib/components/HttpsSettings.svelte';
import SearchIcon from '@lucide/svelte/icons/search';
import DownloadIcon from '@lucide/svelte/icons/download';
import UploadIcon from '@lucide/svelte/icons/upload';

let tab = $state<'security' | 'https' | 'runtime'>('security');
let settings: Record<string, string> = $state({});
let loading = $state(true);
let error = $state<string | null>(null);
let editingKey = $state<string | null>(null);
let editingValue = $state('');
let importText = $state('');
let showImport = $state(false);
let search = $state('');

const settingMeta: Record<string, { label: string; description: string; category: string }> = {
  quota_check_interval: {
    label: 'Quota Check Interval',
    description: 'How often the background scheduler checks provider quotas.',
    category: 'Background Jobs',
  },
  usage_flush_interval: {
    label: 'Usage Flush Interval',
    description: 'How often buffered request logs are flushed to SQLite.',
    category: 'Background Jobs',
  },
  circuit_breaker_cleanup: {
    label: 'Circuit Breaker Cleanup',
    description: 'How often expired circuit breaker states are cleaned up.',
    category: 'Background Jobs',
  },
  default_combo_timeout: {
    label: 'Default Combo Timeout',
    description: 'Maximum time in milliseconds for a combo routing attempt.',
    category: 'Routing',
  },
  max_retries: {
    label: 'Max Retries',
    description: 'Maximum retry attempts for a single request.',
    category: 'Routing',
  },
  log_retention_days: {
    label: 'Log Retention Days',
    description: 'Days to keep request logs before cleanup.',
    category: 'Logging',
  },
};

const defaultValues: Record<string, string> = {
  quota_check_interval: '60s',
  usage_flush_interval: '30s',
  circuit_breaker_cleanup: '5m',
  default_combo_timeout: '30000',
  max_retries: '3',
  log_retention_days: '30',
};

const categories = ['Background Jobs', 'Routing', 'Logging', 'Other'];

onMount(async () => {
  document.title = 'Settings — AxonRouter';
  await loadSettings();
});

async function loadSettings() {
  loading = true;
  error = null;
  try {
    const raw = await settingsApi.list() as Record<string, string> | { data: Record<string, string> };
    const data = raw && typeof raw === 'object' && 'data' in raw && typeof raw.data === 'object' && raw.data !== null
      ? raw.data as Record<string, string>
      : raw as Record<string, string>;
    settings = data;
  } catch (err) {
    error = err instanceof Error ? err.message : 'Failed to load settings';
  } finally {
    loading = false;
  }
}

function formatSettingKey(key: string): string {
  return settingMeta[key]?.label || key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function allKeys(): string[] {
  const known = Object.keys(settingMeta);
  const extras = Object.keys(settings).filter((k) => !known.includes(k));
  return [...known, ...extras];
}

function filteredKeys(): string[] {
  const q = search.trim().toLowerCase();
  if (!q) return allKeys();
  return allKeys().filter((key) => {
    const meta = settingMeta[key];
    return (
      key.toLowerCase().includes(q) ||
      meta?.label.toLowerCase().includes(q) ||
      meta?.description.toLowerCase().includes(q) ||
      (defaultValues[key] ?? settings[key] ?? '').toLowerCase().includes(q)
    );
  });
}

function keysByCategory(category: string): string[] {
  return filteredKeys().filter((key) => (settingMeta[key]?.category || 'Other') === category);
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
    toast.success('Settings imported');
  } catch (err) {
    toast.error('Import failed: ' + (err instanceof Error ? err.message : 'Invalid JSON'));
  }
}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Settings.</h1>
    <p class="text-body-sm text-muted-foreground">Configure runtime behavior, security, and HTTPS.</p>
  </div>

  <Tabs.Root bind:value={tab} class="w-full flex flex-col gap-6">
    <Tabs.List class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
      <Tabs.Trigger value="security" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Security</Tabs.Trigger>
      <Tabs.Trigger value="https" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">HTTPS</Tabs.Trigger>
      <Tabs.Trigger value="runtime" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Runtime</Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="security" class="space-y-6">
      <ChangePasswordCard />

      <Card class="shadow-card border-border/60">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md-strong">Data Management</CardTitle>
          <CardDescription class="text-body-sm">Export, import, and migrate settings data.</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="flex flex-wrap gap-2">
            <Button onclick={handleExport} variant="outline" size="sm" class="text-body-sm rounded-sm gap-2">
              <DownloadIcon class="size-4" />
              Export settings (JSON)
            </Button>
            <Button onclick={() => showImport = !showImport} variant="outline" size="sm" class="text-body-sm rounded-sm gap-2">
              <UploadIcon class="size-4" />
              {showImport ? 'Cancel import' : 'Import settings'}
            </Button>
          </div>

          {#if showImport}
            <div class="space-y-3 rounded-xl border border-border bg-card p-4">
              <Label class="text-body-sm-strong">Paste settings JSON</Label>
              <Textarea
                class="w-full h-32 font-mono text-xs"
                placeholder="Paste JSON settings object here..."
                bind:value={importText}
              />
              <div class="flex gap-2">
                <Button onclick={handleImport} disabled={!importText.trim()} size="sm" class="text-body-sm rounded-sm">Import</Button>
                <Button onclick={() => { showImport = false; importText = ''; }} variant="ghost" size="sm" class="text-body-sm">Cancel</Button>
              </div>
            </div>
          {/if}
        </CardContent>
      </Card>
    </Tabs.Content>

    <Tabs.Content value="https">
      <HttpsSettings />
    </Tabs.Content>

    <Tabs.Content value="runtime" class="space-y-6">
      <Card class="shadow-card border-border/60">
        <CardHeader class="pb-3">
          <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <CardTitle class="text-body-md-strong">Runtime Settings</CardTitle>
              <CardDescription class="text-body-sm">Runtime settings stored in SQLite. Changes apply immediately.</CardDescription>
            </div>
            <div class="flex items-center gap-2">
              <div class="relative">
                <SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
                <Input
                  type="text"
                  placeholder="Search settings…"
                  class="h-9 w-full sm:w-64 pl-9 text-body-sm"
                  bind:value={search}
                />
              </div>
              <Button onclick={loadSettings} variant="ghost" size="sm" class="text-body-sm rounded-sm">Reload</Button>
            </div>
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
          {:else if filteredKeys().length === 0}
            <p class="text-body-sm text-muted-foreground py-6 text-center">No settings match your search.</p>
          {:else}
            <div class="grid grid-cols-1 gap-4">
              {#each categories as category}
                {@const keys = keysByCategory(category)}
                {#if keys.length > 0}
                  <Card class="border-border/60 shadow-sm">
                    <CardHeader class="py-3">
                      <CardTitle class="text-body-sm-strong text-muted-foreground">{category}</CardTitle>
                    </CardHeader>
                    <CardContent class="p-0">
                      {#each keys as key, i}
                        {@const value = settings[key] ?? defaultValues[key] ?? ''}
                        <div class="flex items-center justify-between gap-4 px-4 py-3 border-b border-border last:border-0 hover:bg-accent/5 transition-colors">
                          <div class="min-w-0 flex-1 space-y-0.5">
                            <h3 class="text-body-sm-strong">{formatSettingKey(key)}</h3>
                            <p class="text-caption-mono text-muted-foreground">{key}</p>
                            {#if settingMeta[key]?.description}
                              <p class="text-caption text-muted-foreground/60">{settingMeta[key].description}</p>
                            {/if}
                          </div>
                          <div class="flex items-center gap-2 shrink-0">
                            {#if editingKey === key}
                              <Input
                                type="text"
                                class="w-36 h-8 text-body-sm font-mono"
                                bind:value={editingValue}
                                onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && saveEdit()}
                              />
                              <Button onclick={saveEdit} size="sm" class="h-8 text-body-sm rounded-sm">Save</Button>
                              <Button onclick={cancelEdit} variant="ghost" size="sm" class="h-8 text-body-sm">Cancel</Button>
                            {:else}
                              <span class="text-code font-mono text-muted-foreground mr-2">{value}</span>
                              <Button onclick={() => startEdit(key, value)} variant="ghost" size="sm" class="h-8 text-body-sm rounded-sm">Edit</Button>
                            {/if}
                          </div>
                        </div>
                      {/each}
                    </CardContent>
                  </Card>
                {/if}
              {/each}
            </div>
          {/if}
        </CardContent>
      </Card>
    </Tabs.Content>
  </Tabs.Root>
</div>
