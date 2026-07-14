<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Label } from '$lib/components/ui/label';
  import { Textarea } from '$lib/components/ui/textarea';
  import { Switch } from '$lib/components/ui/switch';
  import * as Select from '$lib/components/ui/select';
import * as Tabs from '$lib/components/ui/tabs';
  import { Badge } from '$lib/components/ui/badge';
import { compressionApi, cacheApi } from '$lib/api';
import type { CompressionSettings, CacheStats, CompressionPreviewResult } from '$lib/api';
import { toast } from 'svelte-sonner';
import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
import InfoIcon from '@lucide/svelte/icons/info';

  let compression = $state<CompressionSettings>({ mode: 'off' });
  let cacheStats = $state<CacheStats>({ hits: 0, misses: 0, size: 0, hit_rate: 0 });
  let previewBody = $state('');
  let previewResult = $state<CompressionPreviewResult | null>(null);
    let loading = $state(false);
  let saving = $state(false);

  let liteCollapse = $state(true);
  let liteImageUrls = $state(true);
  let liteRedundant = $state(false);
  let liteDedup = $state(false);

  onMount(async () => {
    document.title = 'Optimization — AxonRouter';
    await Promise.all([loadCompression(), loadCacheStats()]);
  });

  async function loadCompression() {
    try {
      compression = await compressionApi.getSettings();
      if (compression.lite) {
        liteCollapse = compression.lite.collapse_whitespace ?? true;
        liteImageUrls = compression.lite.replace_image_urls ?? true;
        liteRedundant = compression.lite.remove_redundant_content ?? false;
        liteDedup = compression.lite.dedup_system_prompt ?? false;
      }
    } catch { /* keep defaults */ }
  }

  async function loadCacheStats() {
    try {
      cacheStats = await cacheApi.stats();
    } catch { /* keep defaults */ }
  }

  async function saveCompression() {
    saving = true;
    try {
      compression = await compressionApi.updateSettings({
        mode: compression.mode,
        lite: {
          collapse_whitespace: liteCollapse,
          replace_image_urls: liteImageUrls,
          remove_redundant_content: liteRedundant,
          dedup_system_prompt: liteDedup,
        },
      });
      toast.success('Compression settings saved');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to save');
    } finally {
      saving = false;
    }
  }

  async function runPreview() {
    if (!previewBody.trim()) return;
    loading = true;
    try {
      previewResult = await compressionApi.preview({
        body: previewBody,
        mode: compression.mode || 'standard',
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Preview failed');
    } finally {
      loading = false;
    }
  }

  async function flushCache() {
    try {
      await cacheApi.flush();
      await loadCacheStats();
      toast.success('Cache flushed');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to flush cache');
    }
  }

  const modes = [
    { value: 'off', label: 'Off' },
    { value: 'lite', label: 'Lite' },
    { value: 'standard', label: 'Standard' },
  ];
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Optimization.</h1>
    <p class="text-body-sm text-muted-foreground">
      Token compression and response caching to reduce upstream costs.
    </p>
  </div>

  <Tabs.Root value="compression" class="w-full flex flex-col gap-6">
  <Tabs.List class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
    <Tabs.Trigger value="compression" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Compression</Tabs.Trigger>
    <Tabs.Trigger value="cache" class="rounded-md px-4 py-1.5 text-body-sm font-medium data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm">Cache</Tabs.Trigger>
  </Tabs.List>
  <Tabs.Content value="compression">
    <div class="space-y-4">
      <Card class="shadow-card">
        <CardHeader class="pb-3">
          <CardTitle class="text-base">Compression Mode</CardTitle>
          <CardDescription class="text-xs">
            Lite strips whitespace and image data URLs. Standard adds Caveman rule-based prose condensation.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="grid gap-2">
            <Label for="mode">Mode</Label>
            <Select.Root type="single" bind:value={compression.mode}>
              <Select.Trigger class="w-full">
                {modes.find((m) => m.value === compression.mode)?.label ?? 'Select mode'}
              </Select.Trigger>
              <Select.Content>
                {#each modes as mode}
                  <Select.Item value={mode.value}>{mode.label}</Select.Item>
                {/each}
              </Select.Content>
            </Select.Root>
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-card">
        <CardHeader class="pb-3">
          <CardTitle class="text-base">Lite Options</CardTitle>
        </CardHeader>
        <CardContent class="space-y-4">
<div class="flex items-center justify-between">
			<Label for="collapse" class="cursor-pointer">Collapse whitespace</Label>
			<Switch id="collapse" checked={liteCollapse} onCheckedChange={(v) => (liteCollapse = v)} />
		</div>
		<div class="flex items-center justify-between">
			<Label for="image-urls" class="cursor-pointer">Replace image data URLs</Label>
			<Switch id="image-urls" checked={liteImageUrls} onCheckedChange={(v) => (liteImageUrls = v)} />
		</div>
		<div class="flex items-center justify-between">
			<Label for="redundant" class="cursor-pointer">Remove redundant content</Label>
			<Switch id="redundant" checked={liteRedundant} onCheckedChange={(v) => (liteRedundant = v)} />
		</div>
		<div class="flex items-center justify-between">
			<Label for="dedup" class="cursor-pointer">Deduplicate system prompts</Label>
			<Switch id="dedup" checked={liteDedup} onCheckedChange={(v) => (liteDedup = v)} />
		</div>
          <div class="pt-2">
            <Button onclick={saveCompression} disabled={saving}>
              {saving ? 'Saving...' : 'Save Settings'}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-card">
        <CardHeader class="pb-3">
          <CardTitle class="text-base">Preview</CardTitle>
          <CardDescription class="text-xs">
            Paste an OpenAI-style request JSON to test compression.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <Textarea
            bind:value={previewBody}
            placeholder="Paste an OpenAI-style request JSON here"
            rows={6}
          />
          <Button onclick={runPreview} disabled={loading}>
            {loading ? 'Compressing...' : 'Compress'}
          </Button>

          {#if previewResult}
            <div class="grid grid-cols-2 gap-4 pt-2">
              <div>
                <p class="text-muted-foreground text-xs mb-1">Original tokens</p>
                <p class="text-lg font-semibold">{previewResult.original_tokens}</p>
              </div>
              <div>
                <p class="text-muted-foreground text-xs mb-1">Compressed tokens</p>
                <p class="text-lg font-semibold">{previewResult.compressed_tokens}</p>
              </div>
            </div>
            <div class="flex items-center gap-2 pt-1">
              <Badge variant={previewResult.savings_percent > 0 ? 'default' : 'secondary'}>
                {previewResult.savings_percent.toFixed(1)}% saved
              </Badge>
              {#if previewResult.techniques_used.length > 0}
                <span class="text-muted-foreground text-xs">
                  {previewResult.techniques_used.join(', ')}
                </span>
              {/if}
            </div>
            <div class="bg-muted rounded-md p-3 mt-2">
              <p class="text-muted-foreground text-xs mb-1">Compressed output</p>
              <pre class="text-xs whitespace-pre-wrap break-all">{previewResult.compressed}</pre>
            </div>
          {/if}
        </CardContent>
      </Card>
    </div>
  </Tabs.Content>
<Tabs.Content value="cache">
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h2 class="text-display-md">Cache Statistics</h2>
        <p class="text-body-sm text-muted-foreground">Response cache metrics and management.</p>
      </div>
      <div class="flex items-center gap-2">
        <Button onclick={loadCacheStats} variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer">
          <RefreshCwIcon class="size-3.5 mr-1.5" /> Refresh
        </Button>
        <Button onclick={flushCache} variant="destructive" size="sm" class="text-body-sm rounded-sm cursor-pointer">
          Flush Cache
        </Button>
      </div>
    </div>

    <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
      <div class="bg-card rounded-xl shadow-card p-4">
        <p class="text-caption-mono text-muted-foreground uppercase">Hits</p>
        <p class="text-display-md font-semibold mt-1">{cacheStats.hits.toLocaleString()}</p>
      </div>
      <div class="bg-card rounded-xl shadow-card p-4">
        <p class="text-caption-mono text-muted-foreground uppercase">Misses</p>
        <p class="text-display-md font-semibold mt-1">{cacheStats.misses.toLocaleString()}</p>
      </div>
      <div class="bg-card rounded-xl shadow-card p-4">
        <p class="text-caption-mono text-muted-foreground uppercase">Hit Rate</p>
        <p class="text-display-md font-semibold mt-1">{cacheStats.hit_rate.toFixed(1)}%</p>
      </div>
      <div class="bg-card rounded-xl shadow-card p-4">
        <p class="text-caption-mono text-muted-foreground uppercase">Entries</p>
        <p class="text-display-md font-semibold mt-1">{cacheStats.size}</p>
      </div>
    </div>

    <div class="flex items-start gap-3 rounded-xl bg-muted p-4">
      <InfoIcon class="size-4 mt-0.5 shrink-0 text-muted-foreground" />
      <p class="text-body-sm text-muted-foreground">
        Cache is active only for non-streaming requests that do not include
        <code class="bg-background px-1 py-0.5 rounded text-caption-mono">tools</code>
        or
        <code class="bg-background px-1 py-0.5 rounded text-caption-mono">cache_control</code>.
      </p>
    </div>
  </div>
</Tabs.Content>
</Tabs.Root>
</div>
