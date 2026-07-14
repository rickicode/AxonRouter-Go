<script lang="ts">
import { onMount } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Label } from '$lib/components/ui/label';
import { Textarea } from '$lib/components/ui/textarea';
import * as Tabs from '$lib/components/ui/tabs';
import { settingsApi } from '$lib/api';
import { toast } from 'svelte-sonner';
import ChangePasswordCard from '$lib/components/ChangePasswordCard.svelte';
import HttpsSettings from '$lib/components/HttpsSettings.svelte';
import RuntimeSettings from '$lib/components/RuntimeSettings.svelte';
import DownloadIcon from '@lucide/svelte/icons/download';
import UploadIcon from '@lucide/svelte/icons/upload';

let tab = $state<'runtime' | 'security' | 'https'>('runtime');
let settings: Record<string, string> = $state({});
let importText = $state('');
let showImport = $state(false);

onMount(async () => {
  document.title = 'Settings — AxonRouter';
  await loadSettings();
});

async function loadSettings() {
  try {
    const raw = await settingsApi.list() as Record<string, string> | { data: Record<string, string> };
    const data = raw && typeof raw === 'object' && 'data' in raw && typeof raw.data === 'object' && raw.data !== null
      ? raw.data as Record<string, string>
      : raw as Record<string, string>;
    settings = data;
  } catch (err) {
    toast.error('Failed to load settings: ' + (err instanceof Error ? err.message : 'Unknown'));
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
    <p class="text-body-sm text-muted-foreground">Manage runtime parameters, security, and HTTPS.</p>
  </div>

  <Tabs.Root bind:value={tab} class="w-full flex flex-col gap-6">
    <Tabs.List class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
      <Tabs.Trigger value="runtime" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Runtime</Tabs.Trigger>
      <Tabs.Trigger value="security" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Security</Tabs.Trigger>
      <Tabs.Trigger value="https" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">HTTPS</Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="runtime">
      <RuntimeSettings />
    </Tabs.Content>

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
  </Tabs.Root>
</div>
