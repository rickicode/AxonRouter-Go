<script lang="ts">
  import { onMount } from 'svelte';
  import * as Dialog from '$lib/components/ui/dialog';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Textarea } from '$lib/components/ui/textarea';
  import { Badge } from '$lib/components/ui/badge';
  import { ScrollArea } from '$lib/components/ui/scroll-area';
  import { Skeleton } from '$lib/components/ui/skeleton';
  import { toast } from 'svelte-sonner';
  import { Copy, Check, ChevronRight, ExternalLink, Search, ArrowLeft } from '@lucide/svelte';
  import { cliToolsApi, modelsApi, apiKeysApi } from '$lib/api';
  import type {
    CLITool,
    GatewayModel,
    APIKeyItem,
    CLIToolSelection,
    CLIToolConfig,
    CLIToolStatus,
  } from '$lib/api';

  // --- State ---
  let tools = $state<CLITool[]>([]);
  let models = $state<GatewayModel[]>([]);
  let keys = $state<APIKeyItem[]>([]);
  let statuses = $state<Record<string, CLIToolStatus>>({});
  let loading = $state(true);
  let pageError = $state<string | null>(null);

  // Selected tool for the detail modal
  let selectedTool = $state<CLITool | null>(null);
  let detailOpen = $state(false);

  // Detail modal state
  let sel = $state<CLIToolSelection>({ model: '', apiKeyId: '', baseUrl: '' });
  let configured = $state(false);
  let apiKeyValue = $state('');
  let generated = $state<CLIToolConfig | null>(null);
  let generating = $state(false);
  let copiedEnv = $state(false);
  let copiedConfig = $state(false);
  let copiedModel = $state(false);

  // Model picker modal state
  let modelModalOpen = $state(false);
  let modelSearch = $state('');

  const defaultBaseUrl =
    typeof window !== 'undefined' ? `${window.location.origin}/v1` : 'http://localhost:3777/v1';

  onMount(() => {
    document.title = 'CLI Tools — AxonRouter';
    loadAll();
  });

  async function loadAll() {
    loading = true;
    pageError = null;
    try {
      const [toolsRes, modelsRes, keysRes, statusRes] = await Promise.all([
        cliToolsApi.list(),
        modelsApi.list(),
        apiKeysApi.list(),
        cliToolsApi.statuses(),
      ]);
      tools = toolsRes.data ?? [];
      models = modelsRes.data ?? [];
      keys = keysRes.data ?? [];
      statuses = statusRes ?? {};
    } catch (err) {
      pageError = err instanceof Error ? err.message : 'Failed to load CLI tools';
      toast.error(pageError);
    } finally {
      loading = false;
    }
  }

  async function openTool(tool: CLITool) {
    selectedTool = tool;
    detailOpen = true;
    generated = null;
    apiKeyValue = '';
    copiedEnv = false;
    copiedConfig = false;
    generating = false;
    try {
      const res = await cliToolsApi.get(tool.id);
      sel = {
        model: res.selection?.model ?? '',
        apiKeyId: res.selection?.apiKeyId ?? '',
        baseUrl: res.selection?.baseUrl || res.defaultBaseUrl || defaultBaseUrl,
      };
      configured = res.configured;
    } catch {
      sel = { model: '', apiKeyId: '', baseUrl: defaultBaseUrl };
      configured = false;
    }
  }

  async function generate() {
    if (!selectedTool) return;
    if (!sel.model) {
      toast.error('Please select a model');
      return;
    }
    generating = true;
    try {
      const res = await cliToolsApi.save(selectedTool.id, {
        ...sel,
        apiKeyValue,
      });
      sel = res.selection;
      generated = res.config;
      configured = true;
      statuses[selectedTool.id] = { configured: true };
      toast.success('Config generated');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to generate config');
    } finally {
      generating = false;
    }
  }

  async function copyText(text: string, field: 'copiedEnv' | 'copiedConfig' | 'copiedModel') {
    if (!text) return;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.left = '-9999px';
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      if (field === 'copiedEnv') copiedEnv = true;
      if (field === 'copiedConfig') copiedConfig = true;
      if (field === 'copiedModel') copiedModel = true;
      toast.success('Copied to clipboard');
      setTimeout(() => {
        copiedEnv = false;
        copiedConfig = false;
        copiedModel = false;
      }, 2000);
    } catch {
      toast.error('Failed to copy');
    }
  }

  function openModelPicker() {
    modelSearch = '';
    modelModalOpen = true;
  }

  function pickModel(modelId: string) {
    sel.model = modelId;
    modelModalOpen = false;
  }

  let filteredModels = $derived(
    models.filter((m) =>
      modelSearch
        ? m.id.toLowerCase().includes(modelSearch.toLowerCase())
        : true,
    ),
  );

  function getStatusBadge(toolId: string) {
    const isConfigured = statuses[toolId]?.configured;
    if (isConfigured) {
      return { label: 'Connected', class: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' };
    }
    return { label: 'Not configured', class: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20' };
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Header -->
  <div class="space-y-1">
    <h1 class="text-display-lg">CLI Tools.</h1>
    <p class="text-body-sm text-muted-foreground">
      Pick a gateway model and API key, then generate ready-to-use config snippets for popular AI CLIs.
    </p>
  </div>

  {#if pageError}
    <div class="rounded-xl border border-destructive/30 bg-destructive/10 p-4 text-body-sm text-destructive">
      {pageError}
    </div>
  {/if}

  <!-- Grid of tool cards -->
  {#if loading}
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each Array(6) as _}
        <Skeleton class="h-20 rounded-xl" />
      {/each}
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {#each tools as tool (tool.id)}
        {@const s = getStatusBadge(tool.id)}
        <button
          class="group flex items-center gap-3 rounded-xl border border-border bg-card p-3 text-left shadow-card transition-all hover:border-primary/50 hover:shadow-elevated cursor-pointer"
          onclick={() => openTool(tool)}
        >
          <div class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-background/50">
            <img
              src="{tool.image}"
              alt={tool.name}
              class="size-8 rounded-lg object-contain"
              onerror={(e) => { e.currentTarget.style.display = 'none'; }}
            />
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <h3 class="truncate text-body-sm-strong">{tool.name}</h3>
            </div>
            <span class="mt-1 inline-block rounded-full border px-1.5 py-0.5 text-[10px] font-medium {s.class}">
              {s.label}
            </span>
          </div>
          <ChevronRight class="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
        </button>
      {/each}
    </div>
  {/if}
</div>

<!-- Tool Detail Dialog -->
<Dialog.Root bind:open={detailOpen}>
  <Dialog.Content class="sm:max-w-2xl max-h-[90vh] overflow-hidden flex flex-col gap-0 p-0">
    <!-- Header -->
    <div class="flex items-center gap-3 border-b border-border p-4">
      <div class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-background/50">
        {#if selectedTool}
          <img
            src="{selectedTool.image}"
            alt={selectedTool.name}
            class="size-8 rounded-lg object-contain"
            onerror={(e) => { e.currentTarget.style.display = 'none'; }}
          />
        {/if}
      </div>
      <div class="min-w-0 flex-1">
        <Dialog.Title class="text-body-md-strong">{selectedTool?.name}</Dialog.Title>
        <Dialog.Description class="text-caption text-muted-foreground">{selectedTool?.description}</Dialog.Description>
      </div>
      {#if selectedTool}
        {@const s = getStatusBadge(selectedTool.id)}
        <Badge variant="outline" class="border-0 {s.class}">{s.label}</Badge>
      {/if}
    </div>

    <!-- Body -->
    <ScrollArea class="flex-1 max-h-[60vh]">
      <div class="flex flex-col gap-4 p-4">
        {#if selectedTool?.docsUrl}
          <a
            href={selectedTool.docsUrl}
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1.5 text-caption text-muted-foreground hover:text-primary"
          >
            <ExternalLink class="size-3" />
            Documentation
          </a>
        {/if}

        <!-- Model picker -->
        <div class="space-y-2">
          <Label class="text-caption-mono uppercase text-muted-foreground">Model</Label>
          <div class="flex gap-2">
            <Input
              bind:value={sel.model}
              placeholder="provider/model-id"
              class="font-mono text-body-sm flex-1"
            />
            <Button
              variant="outline"
              size="sm"
              class="shrink-0 gap-1.5"
              onclick={openModelPicker}
              disabled={models.length === 0}
            >
              <Search class="size-3.5" />
              Browse
            </Button>
            {#if sel.model}
              <Button
                variant="ghost"
                size="sm"
                class="shrink-0"
                onclick={() => copyText(sel.model, 'copiedModel')}
              >
                {#if copiedModel}
                  <Check class="size-3.5 text-emerald-400" />
                {:else}
                  <Copy class="size-3.5" />
                {/if}
              </Button>
            {/if}
          </div>
        </div>

        <!-- API Key picker -->
        <div class="space-y-2">
          <Label class="text-caption-mono uppercase text-muted-foreground">API Key</Label>
          <div class="flex gap-2">
            <select
              bind:value={sel.apiKeyId}
              class="flex-1 rounded-sm border border-border bg-background px-3 py-2 text-body-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary/50"
            >
              <option value="">— Select API key —</option>
              {#each keys as key}
                <option value={key.id}>{key.name || 'Untitled'} ({key.key_preview})</option>
              {/each}
            </select>
          </div>
        </div>

        <!-- Raw API Key value -->
        <div class="space-y-2">
          <Label class="text-caption-mono uppercase text-muted-foreground">Raw API Key Value</Label>
          <Input
            type="password"
            bind:value={apiKeyValue}
            placeholder="Paste your AxonRouter API key value (not stored)"
            class="text-body-sm"
          />
          <p class="text-caption text-muted-foreground">
            AxonRouter stores only bcrypt hashes, so the raw value cannot be retrieved. Paste it once to embed it in the generated config.
          </p>
        </div>

        <!-- Base URL -->
        <div class="space-y-2">
          <Label class="text-caption-mono uppercase text-muted-foreground">Gateway Base URL</Label>
          <Input bind:value={sel.baseUrl} placeholder={defaultBaseUrl} class="font-mono text-body-sm" />
        </div>

        <!-- Generate button -->
        <div class="flex items-center gap-2 pt-2">
          <Button
            variant="default"
            size="sm"
            class="text-body-sm rounded-sm cursor-pointer"
            onclick={generate}
            disabled={generating || !sel.model}
          >
            {generating ? 'Generating…' : 'Generate config'}
          </Button>
        </div>

        <!-- Generated config output -->
        {#if generated}
          <div class="mt-2 space-y-4 rounded-lg border border-border bg-background/50 p-4">
            <h3 class="text-body-sm-strong">Generated config</h3>

            <!-- Env block -->
            {#if generated.envBlock}
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-caption-mono text-muted-foreground">Environment variables</Label>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-7 gap-1.5 text-caption"
                    onclick={() => copyText(generated!.envBlock, 'copiedEnv')}
                  >
                    {#if copiedEnv}
                      <Check class="size-3.5" /> Copied
                    {:else}
                      <Copy class="size-3.5" /> Copy
                    {/if}
                  </Button>
                </div>
                <Textarea
                  readonly
                  value={generated.envBlock}
                  rows={Math.min(8, generated.envBlock.split('\n').length)}
                  class="font-mono text-body-sm bg-background"
                />
              </div>
            {/if}

            <!-- Config file -->
            {#if generated.configContent}
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-caption-mono text-muted-foreground">
                    Config file
                    {#if generated.configPath}
                      <span class="text-muted-foreground/70"> · {generated.configPath}</span>
                    {/if}
                  </Label>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-7 gap-1.5 text-caption"
                    onclick={() => copyText(generated!.configContent, 'copiedConfig')}
                  >
                    {#if copiedConfig}
                      <Check class="size-3.5" /> Copied
                    {:else}
                      <Copy class="size-3.5" /> Copy
                    {/if}
                  </Button>
                </div>
                <Textarea
                  readonly
                  value={generated.configContent}
                  rows={Math.min(14, generated.configContent.split('\n').length)}
                  class="font-mono text-body-sm bg-background"
                />
              </div>
            {/if}

            <!-- Run command -->
            {#if generated.runCommand}
              <div class="space-y-2">
                <Label class="text-caption-mono text-muted-foreground">Example command</Label>
                <div class="rounded-md border border-border bg-background px-3 py-2 font-mono text-body-sm">
                  {generated.runCommand}
                </div>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    </ScrollArea>
  </Dialog.Content>
</Dialog.Root>

<!-- Model Picker Dialog -->
<Dialog.Root bind:open={modelModalOpen}>
  <Dialog.Content class="sm:max-w-lg max-h-[80vh] overflow-hidden flex flex-col gap-0 p-0">
    <div class="border-b border-border p-4">
      <Dialog.Title class="text-body-md-strong">Select model</Dialog.Title>
      <Dialog.Description class="text-caption text-muted-foreground">
        Browse all models available on this gateway.
      </Dialog.Description>
      <div class="mt-3 flex items-center gap-2">
        <Search class="size-4 text-muted-foreground" />
        <Input
          bind:value={modelSearch}
          placeholder="Search models…"
          class="text-body-sm"
        />
      </div>
    </div>
    <ScrollArea class="flex-1 max-h-[50vh]">
      <div class="flex flex-col">
        {#each filteredModels as model (model.id)}
          <button
            class="flex items-center justify-between border-b border-border/50 px-4 py-2.5 text-left text-body-sm font-mono hover:bg-card/50 transition-colors cursor-pointer {sel.model === model.id ? 'bg-primary/5 text-primary' : ''}"
            onclick={() => pickModel(model.id)}
          >
            <span class="truncate">{model.id}</span>
            <span class="ml-2 shrink-0 text-caption text-muted-foreground">{model.owned_by}</span>
          </button>
        {/each}
        {#if filteredModels.length === 0}
          <div class="px-4 py-6 text-center text-body-sm text-muted-foreground">
            No models found.
          </div>
        {/if}
      </div>
    </ScrollArea>
  </Dialog.Content>
</Dialog.Root>
