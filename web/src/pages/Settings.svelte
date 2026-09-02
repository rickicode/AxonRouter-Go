<script lang="ts">
import { onMount } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Label } from '$lib/components/ui/label';
import { Textarea } from '$lib/components/ui/textarea';
import * as Tabs from '$lib/components/ui/tabs';
import { settingsApi } from '$lib/api';
import { toast } from 'svelte-sonner';
import { t, getT } from '$lib/i18n';
import ChangePasswordCard from '$lib/components/ChangePasswordCard.svelte';
import HttpsSettings from '$lib/components/HttpsSettings.svelte';
import RuntimeSettings from '$lib/components/RuntimeSettings.svelte';
import SmartRouterSettings from '$lib/components/SmartRouterSettings.svelte';
import SparklesIcon from '@lucide/svelte/icons/sparkles';
import DownloadIcon from '@lucide/svelte/icons/download';
import UploadIcon from '@lucide/svelte/icons/upload';

let tab = $state<'runtime' | 'security' | 'https' | 'smart-router'>('runtime');
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
    toast.success(getT()('settings.imported'));
  } catch (err) {
    toast.error(getT()('settings.importFailed', { message: err instanceof Error ? err.message : 'Invalid JSON' }));
  }
}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">{$t('settings.title')}</h1>
    <p class="text-body-sm text-muted-foreground">{$t('settings.subtitle')}</p>
  </div>

  <Tabs.Root bind:value={tab} class="w-full flex flex-col gap-6">
    <Tabs.List class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
      <Tabs.Trigger value="runtime" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">{$t('settings.tabs.runtime')}</Tabs.Trigger>
      <Tabs.Trigger value="security" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">{$t('settings.tabs.security')}</Tabs.Trigger>
      <Tabs.Trigger value="https" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">{$t('settings.tabs.https')}</Tabs.Trigger>
<Tabs.Trigger value="smart-router" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm gap-1.5">
<SparklesIcon class="size-4" />
{$t('settings.tabs.smartRouter')}
</Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="runtime">
      <RuntimeSettings />
    </Tabs.Content>

    <Tabs.Content value="security" class="space-y-6">
      <ChangePasswordCard />

      <Card class="shadow-card border-border/60">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md-strong">{$t('settings.dataManagement')}</CardTitle>
          <CardDescription class="text-body-sm">{$t('settings.dataManagementDesc')}</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="flex flex-wrap gap-2">
            <Button onclick={handleExport} variant="outline" size="sm" class="text-body-sm rounded-sm gap-2">
              <DownloadIcon class="size-4" />
              {$t('settings.exportButton')}
            </Button>
            <Button onclick={() => showImport = !showImport} variant="outline" size="sm" class="text-body-sm rounded-sm gap-2">
              <UploadIcon class="size-4" />
              {showImport ? $t('settings.cancelImport') : $t('settings.importButton')}
            </Button>
          </div>

          {#if showImport}
            <div class="space-y-3 rounded-xl border border-border bg-card p-4">
              <Label class="text-body-sm-strong">{$t('settings.importLabel')}</Label>
              <Textarea
                class="w-full h-32 font-mono text-xs"
                placeholder="{$t('settings.importPlaceholder')}"
                bind:value={importText}
              />
              <div class="flex gap-2">
                <Button onclick={handleImport} disabled={!importText.trim()} size="sm" class="text-body-sm rounded-sm">{$t('settings.importAction')}</Button>
                <Button onclick={() => { showImport = false; importText = ''; }} variant="ghost" size="sm" class="text-body-sm">{$t('settings.cancel')}</Button>
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
