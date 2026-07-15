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
import type { CompressionSettings, CacheStats, CompressionPreviewResult, CompressionMetrics } from '$lib/api';
import { toast } from 'svelte-sonner';
import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
import InfoIcon from '@lucide/svelte/icons/info';
import CheckIcon from '@lucide/svelte/icons/check';
import XIcon from '@lucide/svelte/icons/x';

let compression = $state<CompressionSettings>({ mode: 'off' });
let cacheStats = $state<CacheStats>({ hits: 0, misses: 0, size: 0, hit_rate: 0 });
let metrics = $state<CompressionMetrics>({
  total_requests: 0,
  original_tokens: 0,
  compressed_tokens: 0,
  tokens_saved: 0,
  savings_percent: 0,
  modes: [],
});
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
	await Promise.all([loadCompression(), loadCacheStats(), loadMetrics()]);
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

async function loadMetrics() {
	try {
		metrics = await compressionApi.metrics();
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
  { value: 'rtk', label: 'RTK' },
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
<!-- Active compression summary -->
<Card class="shadow-card">
<CardContent class="p-4">
<div class="flex flex-col gap-4">
<div class="flex items-center justify-between">
<div>
<h2 class="text-display-md">Active compression</h2>
<p class="text-body-sm text-muted-foreground">Current live settings.</p>
</div>
<Badge variant={compression.mode === 'off' ? 'secondary' : 'default'}>
{modes.find((m) => m.value === compression.mode)?.label ?? 'Unknown'}
</Badge>
</div>
<div class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-body-sm">
<div class="flex items-center gap-2">
{#if liteCollapse}
<CheckIcon class="size-4 text-emerald-500" />
<span>Collapse whitespace</span>
{:else}
<XIcon class="size-4 text-muted-foreground" />
<span class="text-muted-foreground">Collapse whitespace</span>
{/if}
</div>
<div class="flex items-center gap-2">
{#if liteImageUrls}
<CheckIcon class="size-4 text-emerald-500" />
<span>Replace image URLs</span>
{:else}
<XIcon class="size-4 text-muted-foreground" />
<span class="text-muted-foreground">Replace image URLs</span>
{/if}
</div>
<div class="flex items-center gap-2">
{#if liteRedundant}
<CheckIcon class="size-4 text-emerald-500" />
<span>Remove redundant content</span>
{:else}
<XIcon class="size-4 text-muted-foreground" />
<span class="text-muted-foreground">Remove redundant content</span>
{/if}
</div>
<div class="flex items-center gap-2">
{#if liteDedup}
<CheckIcon class="size-4 text-emerald-500" />
<span>Deduplicate system prompts</span>
{:else}
<XIcon class="size-4 text-muted-foreground" />
<span class="text-muted-foreground">Deduplicate system prompts</span>
{/if}
</div>
</div>
<div class="flex items-start gap-2 rounded-lg bg-muted p-3 text-body-sm text-muted-foreground">
<InfoIcon class="size-4 mt-0.5 shrink-0" />
<span>Changing the compression mode requires a server restart to affect API traffic.</span>
</div>
</div>
</CardContent>
</Card>

<Card class="shadow-card">
<CardHeader class="pb-3">
<CardTitle class="text-base">Compression Mode</CardTitle>
<CardDescription class="text-xs">
              Lite strips whitespace and image data URLs. Standard adds Caveman rule-based prose condensation. RTK targets OpenAI, Claude and Responses tool outputs.
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

<!-- Mode details -->
<div class="rounded-lg bg-muted p-3 space-y-2">
<h3 class="text-body-sm-strong text-foreground">How this mode works</h3>
{#if compression.mode === 'off'}
<ul class="list-disc pl-4 text-xs text-muted-foreground space-y-1">
<li>No request compression is applied.</li>
<li>Request bodies are forwarded to upstream unchanged.</li>
<li>Exact response caching is still active when a cached match exists.</li>
</ul>
{:else if compression.mode === 'lite'}
<ul class="list-disc pl-4 text-xs text-muted-foreground space-y-1">
<li>Collapses extra whitespace in message text and text parts.</li>
<li>Replaces <code class="bg-background px-1 py-0.5 rounded text-caption-mono">data:image/...</code> base64 URLs with <code class="bg-background px-1 py-0.5 rounded text-caption-mono">[image]</code> in text and <code class="bg-background px-1 py-0.5 rounded text-caption-mono">image_url.url</code> fields.</li>
<li>Optionally deduplicates repeated system prompts and removes back-to-back identical assistant messages.</li>
<li>Fail-open: if the body cannot be parsed, the original is forwarded unchanged.</li>
</ul>
{:else if compression.mode === 'standard'}
<ul class="list-disc pl-4 text-xs text-muted-foreground space-y-1">
<li>Runs all Lite steps first (whitespace, image URLs, dedup).</li>
<li>Then applies the Caveman rule-based prose compressor (~80 English rules).</li>
<li>Removes filler adverbs, pleasantries, hedging, redundant phrases, and verbose instructions.</li>
<li>Best for general user/assistant chat prose with lots of natural language.</li>
</ul>
{:else if compression.mode === 'rtk'}
<ul class="list-disc pl-4 text-xs text-muted-foreground space-y-1">
<li>Runs all Lite steps first (whitespace, image URLs, dedup).</li>
<li>Then applies the RTK compressor for tool and function-call outputs.</li>
<li>Targets <code class="bg-background px-1 py-0.5 rounded text-caption-mono">tool</code> role messages, <code class="bg-background px-1 py-0.5 rounded text-caption-mono">function_call_output</code> items, <code class="bg-background px-1 py-0.5 rounded text-caption-mono">tool_result</code> parts, and Responses API <code class="bg-background px-1 py-0.5 rounded text-caption-mono">input</code> entries.</li>
<li>Leaves normal user/assistant prose untouched.</li>
</ul>
{:else}
<p class="text-xs text-muted-foreground">Select a mode to see what it does.</p>
{/if}
</div>
</CardContent>
</Card>

<!-- Compression metrics -->
<Card class="shadow-card">
<CardHeader class="pb-3">
<CardTitle class="text-base">Compression Metrics</CardTitle>
<CardDescription class="text-xs">Aggregated stats from real compressed requests.</CardDescription>
</CardHeader>
<CardContent class="space-y-4">
<div class="grid grid-cols-2 md:grid-cols-4 gap-4">
<div class="bg-card rounded-xl shadow-card p-4">
<p class="text-caption-mono text-muted-foreground uppercase">Requests</p>
<p class="text-display-md font-semibold mt-1">{metrics.total_requests.toLocaleString()}</p>
</div>
<div class="bg-card rounded-xl shadow-card p-4">
<p class="text-caption-mono text-muted-foreground uppercase">Original tokens</p>
<p class="text-display-md font-semibold mt-1">{metrics.original_tokens.toLocaleString()}</p>
</div>
<div class="bg-card rounded-xl shadow-card p-4">
<p class="text-caption-mono text-muted-foreground uppercase">Compressed tokens</p>
<p class="text-display-md font-semibold mt-1">{metrics.compressed_tokens.toLocaleString()}</p>
</div>
<div class="bg-card rounded-xl shadow-card p-4">
<p class="text-caption-mono text-muted-foreground uppercase">Tokens saved</p>
<p class="text-display-md font-semibold mt-1">{metrics.tokens_saved.toLocaleString()}</p>
</div>
</div>
<div class="flex items-center gap-2">
<Badge variant={metrics.savings_percent > 0 ? 'default' : 'secondary'}>
{metrics.savings_percent.toFixed(1)}% saved
</Badge>
</div>
{#if metrics.modes.length > 0}
<div class="space-y-2">
<h3 class="text-body-sm-strong">By mode</h3>
<div class="rounded-lg border border-border overflow-hidden">
<table class="w-full text-left text-xs">
<thead class="bg-muted text-muted-foreground">
<tr>
<th class="px-3 py-2 font-medium">Mode</th>
<th class="px-3 py-2 font-medium text-right">Requests</th>
<th class="px-3 py-2 font-medium text-right">Saved tokens</th>
<th class="px-3 py-2 font-medium text-right">Savings</th>
</tr>
</thead>
<tbody>
{#each metrics.modes as mode}
<tr class="border-t border-border">
<td class="px-3 py-2 capitalize">{mode.mode}</td>
<td class="px-3 py-2 text-right">{mode.requests.toLocaleString()}</td>
<td class="px-3 py-2 text-right">{mode.tokens_saved.toLocaleString()}</td>
<td class="px-3 py-2 text-right">{mode.savings_percent.toFixed(1)}%</td>
</tr>
{/each}
</tbody>
</table>
</div>
</div>
{/if}
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
