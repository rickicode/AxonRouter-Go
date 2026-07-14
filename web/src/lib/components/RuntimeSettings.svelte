<script lang="ts">
import { onMount } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { settingsApi } from '$lib/api';
import { toast } from 'svelte-sonner';
import SearchIcon from '@lucide/svelte/icons/search';
import GaugeIcon from '@lucide/svelte/icons/gauge';
import PencilIcon from '@lucide/svelte/icons/pencil';
import CheckIcon from '@lucide/svelte/icons/check';
import XIcon from '@lucide/svelte/icons/x';
import Loader2Icon from '@lucide/svelte/icons/loader-2';

type Category = 'Background Jobs' | 'Routing' | 'Logging';

const settingMeta: Record<string, { label: string; description: string; category: Category }> = {
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

const categories: Category[] = ['Background Jobs', 'Routing', 'Logging'];

let settings: Record<string, string> = $state({});
let loading = $state(true);
let error = $state<string | null>(null);
let search = $state('');
let filter = $state<'all' | Category>('all');
let editingKey = $state<string | null>(null);
let editingValue = $state('');
let savingKey = $state<string | null>(null);

onMount(load);

async function load() {
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

function currentValue(key: string): string {
  return settings[key] ?? defaultValues[key] ?? '';
}

function filteredKeys(): string[] {
  const known = Object.keys(settingMeta);
  const q = search.trim().toLowerCase();
  return known.filter((key) => {
    const meta = settingMeta[key];
    if (filter !== 'all' && meta.category !== filter) return false;
    if (!q) return true;
    return (
      key.toLowerCase().includes(q) ||
      meta.label.toLowerCase().includes(q) ||
      meta.description.toLowerCase().includes(q) ||
      currentValue(key).toLowerCase().includes(q)
    );
  });
}

function startEdit(key: string) {
  editingKey = key;
  editingValue = currentValue(key);
}

function cancelEdit() {
  editingKey = null;
  editingValue = '';
}

async function saveEdit(key: string) {
  savingKey = key;
  try {
    await settingsApi.update(key, editingValue);
    settings[key] = editingValue;
    editingKey = null;
    editingValue = '';
    toast.success(`${settingMeta[key]?.label || key} saved`);
  } catch (err) {
    toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    savingKey = null;
  }
}

function categoryClass(category: Category): string {
  if (category === 'Background Jobs') return 'bg-sky-500/10 text-sky-600 dark:text-sky-400 border-sky-500/20';
  if (category === 'Routing') return 'bg-violet-500/10 text-violet-600 dark:text-violet-400 border-violet-500/20';
  return 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20';
}
</script>

<Card class="shadow-card border-border/60">
  <CardHeader class="pb-3">
    <div class="flex flex-col gap-4">
      <div class="flex items-start gap-3">
        <span class="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
          <GaugeIcon class="size-5" />
        </span>
        <div class="flex-1">
          <CardTitle class="text-body-md-strong">Runtime settings</CardTitle>
          <CardDescription class="text-body-sm">Parameters that control background jobs, routing, and log retention.</CardDescription>
        </div>
        <div class="relative hidden sm:block">
          <SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search runtime settings…"
            class="h-9 w-full sm:w-64 pl-9 text-body-sm"
            bind:value={search}
          />
        </div>
        <Button onclick={load} variant="ghost" size="sm" class="text-body-sm rounded-sm">Reload</Button>
      </div>

      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="inline-flex flex-wrap items-center gap-1 rounded-lg bg-muted p-1">
          <button
            class="rounded-md px-3 py-1.5 text-body-sm font-medium transition-all {filter === 'all' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
            onclick={() => filter = 'all'}
          >
            All
          </button>
          {#each categories as cat}
            <button
              class="rounded-md px-3 py-1.5 text-body-sm font-medium transition-all {filter === cat ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
              onclick={() => filter = cat}
            >
              {cat}
            </button>
          {/each}
        </div>
        <div class="relative sm:hidden">
          <SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search runtime settings…"
            class="h-9 w-full pl-9 text-body-sm"
            bind:value={search}
          />
        </div>
      </div>
    </div>
  </CardHeader>

  <CardContent>
    {#if loading}
      <div class="flex flex-col gap-3">
        {#each Array(5) as _}
          <div class="h-14 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    {:else if error}
      <div class="flex flex-col items-center justify-center py-8">
        <p class="text-body-sm text-muted-foreground mb-4">{error}</p>
        <Button onclick={load} variant="outline" size="sm" class="text-body-sm rounded-sm">Try again</Button>
      </div>
    {:else if filteredKeys().length === 0}
      <p class="text-body-sm text-muted-foreground py-6 text-center">No runtime settings match your filter.</p>
    {:else}
      <div class="rounded-xl border border-border overflow-hidden bg-card">
        <div class="hidden sm:grid sm:grid-cols-[1fr_160px_88px] gap-4 px-4 py-2 bg-muted/50 border-b border-border text-caption-mono text-muted-foreground uppercase font-semibold">
          <span>Setting</span>
          <span>Value</span>
          <span class="text-right">Action</span>
        </div>

        {#each filteredKeys() as key, i}
          {@const meta = settingMeta[key]}
          {@const value = currentValue(key)}
          {@const editing = editingKey === key}
          <div class="flex flex-col sm:grid sm:grid-cols-[1fr_160px_88px] gap-2 sm:gap-4 px-4 py-3 border-b border-border last:border-0 hover:bg-accent/5 transition-colors group">
            <div class="min-w-0 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <h3 class="text-body-sm-strong">{meta.label}</h3>
                <span class="inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide {categoryClass(meta.category)}">
                  {meta.category}
                </span>
              </div>
              <p class="text-caption-mono text-muted-foreground">{key}</p>
              <p class="text-caption text-muted-foreground/60 sm:hidden">{meta.description}</p>
              <p class="text-caption text-muted-foreground/60 hidden sm:block">{meta.description}</p>
            </div>

            <div class="flex items-center">
              {#if editing}
                <Input
                  type="text"
                  class="h-8 w-full text-body-sm font-mono"
                  bind:value={editingValue}
                  onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && saveEdit(key)}
                  disabled={savingKey === key}
                  autofocus
                />
              {:else}
                <button
                  class="text-left w-full truncate rounded-md border border-transparent px-2 py-1 -ml-2 text-code font-mono text-body-sm text-muted-foreground hover:bg-muted hover:border-border transition-colors"
                  onclick={() => startEdit(key)}
                  title="Click to edit"
                >
                  {value}
                </button>
              {/if}
            </div>

            <div class="flex items-center justify-end">
              {#if editing}
                <Button
                  onclick={() => saveEdit(key)}
                  disabled={savingKey === key}
                  size="sm"
                  class="h-8 w-8 p-0 rounded-sm"
                >
                  {#if savingKey === key}
                    <Loader2Icon class="size-4 animate-spin" />
                  {:else}
                    <CheckIcon class="size-4" />
                  {/if}
                </Button>
                <Button
                  onclick={cancelEdit}
                  variant="ghost"
                  size="sm"
                  class="h-8 w-8 p-0 rounded-sm"
                  disabled={savingKey === key}
                >
                  <XIcon class="size-4" />
                </Button>
              {:else}
                <Button
                  onclick={() => startEdit(key)}
                  variant="ghost"
                  size="sm"
                  class="h-8 w-8 p-0 rounded-sm"
                >
                  <PencilIcon class="size-4" />
                </Button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </CardContent>
</Card>
