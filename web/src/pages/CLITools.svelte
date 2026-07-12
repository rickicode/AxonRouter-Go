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
  import { Copy, Check, ChevronRight, ExternalLink, Search, Info, AlertTriangle, XCircle } from '@lucide/svelte';
  import { cliToolsApi, modelsApi, apiKeysApi } from '$lib/api';
  import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
  import type {
    CLITool,
    GatewayModel,
    APIKeyItem,
    CLIToolSelection,
    CLIToolConfig,
    CLIToolStatus,
    DefaultModel,
  } from '$lib/api';

  // --- State ---
  let tools = $state<CLITool[]>([]);
  let models = $state<GatewayModel[]>([]);
  let keys = $state<APIKeyItem[]>([]);
  let statuses = $state<Record<string, CLIToolStatus>>({});
  let loading = $state(true);
  let pageError = $state<string | null>(null);

  // Detail modal
  let selectedTool = $state<CLITool | null>(null);
  let detailOpen = $state(false);
  let sel = $state<CLIToolSelection>({ model: '', apiKeyId: '', baseUrl: '' });
  let configured = $state(false);
  let apiKeyValue = $state('');
  let generated = $state<CLIToolConfig | null>(null);
  let generating = $state(false);
  let copiedField = $state<string | null>(null);

  // Model alias mappings: { alias: gatewayModelId }
  let modelAliases = $state<Record<string, string>>({});

  // Model picker (reusable)
  let modelPickerOpen = $state(false);
  let modelPickerTarget = $state<string>('_main'); // '_main' or alias name

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
    generating = false;
    copiedField = null;
    modelAliases = {};

    try {
      const res = await cliToolsApi.get(tool.id);
      sel = {
        model: res.selection?.model ?? '',
        apiKeyId: res.selection?.apiKeyId ?? '',
        baseUrl: res.selection?.baseUrl || res.defaultBaseUrl || defaultBaseUrl,
        modelAliases: res.selection?.modelAliases,
      };
      configured = res.configured;
      // Restore saved model aliases
      if (res.selection?.modelAliases) {
        modelAliases = { ...res.selection.modelAliases };
      }
    } catch {
      sel = { model: '', apiKeyId: '', baseUrl: defaultBaseUrl };
      configured = false;
    }

    // Initialize alias defaults for tools with defaultModels
    if (tool.defaultModels) {
      for (const dm of tool.defaultModels) {
        if (!modelAliases[dm.alias] && dm.defaultValue) {
          modelAliases[dm.alias] = dm.defaultValue;
        }
      }
    }
  }

  async function generate() {
    if (!selectedTool) return;
    generating = true;
    try {
      const res = await cliToolsApi.save(selectedTool.id, {
        ...sel,
        modelAliases: Object.keys(modelAliases).length > 0 ? modelAliases : undefined,
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

  async function copyText(text: string, field: string) {
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
      copiedField = field;
      toast.success('Copied to clipboard');
      setTimeout(() => (copiedField = null), 2000);
    } catch {
      toast.error('Failed to copy');
    }
  }

  function openModelPicker(target: string) {
    modelPickerTarget = target;
    modelPickerOpen = true;
  }

  function onModelPick(modelId: string) {
    if (modelPickerTarget === '_main') {
      sel.model = modelId;
    } else {
      modelAliases[modelPickerTarget] = modelId;
    }
  }

  // Template variable substitution for code blocks and guide step values
  function replaceVars(text: string): string {
    const key = apiKeyValue || '__YOUR_AXONROUTER_API_KEY__';
    const base = sel.baseUrl ? (sel.baseUrl.endsWith('/v1') ? sel.baseUrl : `${sel.baseUrl}/v1`) : defaultBaseUrl;
    const model = sel.model || getFirstAliasModel() || 'provider/model-id';
    return text
      .replace(/\{\{baseUrl\}\}/g, base)
      .replace(/\{\{apiKey\}\}/g, key)
      .replace(/\{\{model\}\}/g, model);
  }

  function getFirstAliasModel(): string {
    const vals = Object.values(modelAliases).filter(Boolean);
    return vals.length > 0 ? vals[0] : '';
  }

  function getEffectiveModel(): string {
    return sel.model || getFirstAliasModel() || '';
  }

  function getStatusBadge(toolId: string) {
    if (statuses[toolId]?.configured) {
      return { label: 'Connected', cls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' };
    }
    return { label: 'Not configured', cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20' };
  }

  function getNoteIcon(type: string) {
    if (type === 'warning') return AlertTriangle;
    if (type === 'error') return XCircle;
    return Info;
  }

  function getNoteColors(type: string) {
    if (type === 'warning') return 'border-yellow-500/30 bg-yellow-500/10 text-yellow-400';
    if (type === 'error') return 'border-red-500/30 bg-red-500/10 text-red-400';
    return 'border-blue-500/30 bg-blue-500/10 text-blue-400';
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
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
            <img src={tool.image} alt={tool.name} class="size-8 rounded-lg object-contain" onerror={(e) => (e.currentTarget.style.display = 'none')} />
          </div>
          <div class="min-w-0 flex-1">
            <h3 class="truncate text-body-sm-strong">{tool.name}</h3>
            <span class="mt-1 inline-block rounded-full border px-1.5 py-0.5 text-[10px] font-medium {s.cls}">{s.label}</span>
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
          <img src={selectedTool.image} alt={selectedTool.name} class="size-8 rounded-lg object-contain" onerror={(e) => (e.currentTarget.style.display = 'none')} />
        {/if}
      </div>
      <div class="min-w-0 flex-1">
        <Dialog.Title class="text-body-md-strong">{selectedTool?.name}</Dialog.Title>
        <Dialog.Description class="text-caption text-muted-foreground">{selectedTool?.description}</Dialog.Description>
      </div>
      {#if selectedTool}
        {@const s = getStatusBadge(selectedTool.id)}
        <Badge variant="outline" class="border-0 {s.cls} shrink-0">{s.label}</Badge>
      {/if}
    </div>

    <!-- Body -->
    <ScrollArea class="flex-1 max-h-[60vh]">
      <div class="flex flex-col gap-4 p-4">
        <!-- Docs link -->
        {#if selectedTool?.docsUrl}
          <a href={selectedTool.docsUrl} target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-caption text-muted-foreground hover:text-primary">
            <ExternalLink class="size-3" /> Documentation
          </a>
        {/if}

        <!-- Notes -->
        {#if selectedTool?.notes?.length}
          {#each selectedTool.notes as note}
            {@const Icon = getNoteIcon(note.type)}
            <div class="flex items-start gap-2.5 rounded-lg border p-3 {getNoteColors(note.type)}">
              <Icon class="size-4 shrink-0 mt-0.5" />
              <p class="text-body-sm">{note.text}</p>
            </div>
          {/each}
        {/if}

        <!-- GUIDE STEPS UI (for guide-type tools) -->
        {#if (selectedTool?.guideSteps?.length ?? 0) > 0}
          <div class="space-y-3">
            {#each selectedTool.guideSteps as step}
              <div class="flex gap-3">
                <div class="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary text-[11px] font-bold">
                  {step.step}
                </div>
                <div class="min-w-0 flex-1 space-y-1.5">
                  <p class="text-body-sm-strong">{step.title}</p>
                  {#if step.desc}
                    <p class="text-body-sm text-muted-foreground">{step.desc}</p>
                  {/if}
                  {#if step.value && step.copyable}
                    <div class="flex items-center gap-2">
                      <code class="flex-1 rounded-md border border-border bg-background px-2.5 py-1.5 font-mono text-body-sm">{replaceVars(step.value)}</code>
                      <Button variant="ghost" size="sm" class="h-7 shrink-0" onclick={() => copyText(replaceVars(step.value!), `step-${step.step}`)}>
                        {#if copiedField === `step-${step.step}`}
                          <Check class="size-3.5 text-emerald-400" />
                        {:else}
                          <Copy class="size-3.5" />
                        {/if}
                      </Button>
                    </div>
                  {/if}
                  {#if step.type === 'apiKeySelector'}
                    <select bind:value={sel.apiKeyId} class="w-full rounded-sm border border-border bg-background px-3 py-2 text-body-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary/50">
                      <option value="">— Select API key —</option>
                      {#each keys as key}
                        <option value={key.id}>{key.name || 'Untitled'} ({key.key_preview})</option>
                      {/each}
                    </select>
                  {/if}
                  {#if step.type === 'modelSelector'}
                    <div class="flex gap-2">
                      <Input bind:value={sel.model} placeholder="provider/model-id" class="font-mono text-body-sm flex-1" />
                      <Button variant="outline" size="sm" class="shrink-0 gap-1.5" onclick={() => openModelPicker('_main')} disabled={models.length === 0}>
                        <Search class="size-3.5" /> Browse
                      </Button>
                    </div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}

        <!-- Code block with template substitution -->
        {#if selectedTool?.codeBlock}
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <Label class="text-caption-mono text-muted-foreground">Config snippet ({selectedTool.codeBlock.language})</Label>
              <Button variant="ghost" size="sm" class="h-7 gap-1.5 text-caption" onclick={() => copyText(replaceVars(selectedTool!.codeBlock!.code), 'codeblock')}>
                {#if copiedField === 'codeblock'}
                  <Check class="size-3.5" /> Copied
                {:else}
                  <Copy class="size-3.5" /> Copy
                {/if}
              </Button>
            </div>
            <Textarea readonly value={replaceVars(selectedTool.codeBlock.code)} rows={Math.min(16, selectedTool.codeBlock.code.split('\n').length)} class="font-mono text-body-sm bg-background" />
          </div>
        {/if}

        <!-- MODEL ALIAS MAPPING UI (for tools with defaultModels) -->
        {#if (selectedTool?.defaultModels?.length ?? 0) > 0}
          <div class="space-y-3">
            {#each selectedTool.defaultModels as dm}
              <div class="space-y-1.5">
                <Label class="text-caption-mono text-muted-foreground">{dm.name}</Label>
                <div class="flex gap-2">
                  <Input
                    value={modelAliases[dm.alias] ?? dm.defaultValue ?? ''}
                    oninput={(e) => (modelAliases[dm.alias] = e.currentTarget.value)}
                    placeholder={dm.defaultValue || 'provider/model-id'}
                    class="font-mono text-body-sm flex-1"
                  />
                  <Button variant="outline" size="sm" class="shrink-0 gap-1.5" onclick={() => openModelPicker(dm.alias)} disabled={models.length === 0}>
                    <Search class="size-3.5" /> Browse
                  </Button>
                  {#if modelAliases[dm.alias]}
                    <Button variant="ghost" size="sm" class="shrink-0" onclick={() => copyText(modelAliases[dm.alias], `alias-${dm.alias}`)}>
                      {#if copiedField === `alias-${dm.alias}`}
                        <Check class="size-3.5 text-emerald-400" />
                      {:else}
                        <Copy class="size-3.5" />
                      {/if}
                    </Button>
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          <!-- Shared fields for alias tools -->
          <div class="space-y-2">
            <Label class="text-caption-mono uppercase text-muted-foreground">API Key</Label>
            <select bind:value={sel.apiKeyId} class="w-full rounded-sm border border-border bg-background px-3 py-2 text-body-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary/50">
              <option value="">— Select API key —</option>
              {#each keys as key}
                <option value={key.id}>{key.name || 'Untitled'} ({key.key_preview})</option>
              {/each}
            </select>
          </div>

        <!-- SINGLE MODEL UI (for simple tools) -->
        {:else if (selectedTool?.guideSteps?.length ?? 0) === 0 && (selectedTool?.defaultModels?.length ?? 0) === 0}
          <div class="space-y-2">
            <Label class="text-caption-mono uppercase text-muted-foreground">Model</Label>
            <div class="flex gap-2">
              <Input bind:value={sel.model} placeholder="provider/model-id" class="font-mono text-body-sm flex-1" />
              <Button variant="outline" size="sm" class="shrink-0 gap-1.5" onclick={() => openModelPicker('_main')} disabled={models.length === 0}>
                <Search class="size-3.5" /> Browse
              </Button>
              {#if sel.model}
                <Button variant="ghost" size="sm" class="shrink-0" onclick={() => copyText(sel.model, 'model')}>
                  {#if copiedField === 'model'}
                    <Check class="size-3.5 text-emerald-400" />
                  {:else}
                    <Copy class="size-3.5" />
                  {/if}
                </Button>
              {/if}
            </div>
          </div>
          <div class="space-y-2">
            <Label class="text-caption-mono uppercase text-muted-foreground">API Key</Label>
            <select bind:value={sel.apiKeyId} class="w-full rounded-sm border border-border bg-background px-3 py-2 text-body-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary/50">
              <option value="">— Select API key —</option>
              {#each keys as key}
                <option value={key.id}>{key.name || 'Untitled'} ({key.key_preview})</option>
              {/each}
            </select>
          </div>
        {/if}
<!-- Shared: Raw API Key Value + Base URL (render once for every tool) -->
<div class="space-y-2">
  <Label class="text-caption-mono uppercase text-muted-foreground">Raw API Key Value</Label>
  <Input type="password" bind:value={apiKeyValue} placeholder="Paste your AxonRouter API key value (not stored)" class="text-body-sm" />
  <p class="text-caption text-muted-foreground">AxonRouter stores only bcrypt hashes. Paste the raw value to embed in generated config.</p>
</div>
<div class="space-y-2">
  <Label class="text-caption-mono uppercase text-muted-foreground">Gateway Base URL</Label>
  <Input bind:value={sel.baseUrl} placeholder={defaultBaseUrl} class="font-mono text-body-sm" />
</div>

        <!-- Generate button (always shown) -->
        <div class="flex items-center gap-2 pt-2">
          <Button
            variant="default"
            size="sm"
            class="text-body-sm rounded-sm cursor-pointer"
            onclick={generate}
            disabled={generating}
          >
            {generating ? 'Generating…' : 'Generate config'}
          </Button>
        </div>

        <!-- Generated config output -->
        {#if generated}
          <div class="mt-2 space-y-4 rounded-lg border border-border bg-background/50 p-4">
            <h3 class="text-body-sm-strong">Generated config</h3>

            {#if generated.envBlock}
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-caption-mono text-muted-foreground">Environment variables</Label>
                  <Button variant="ghost" size="sm" class="h-7 gap-1.5 text-caption" onclick={() => copyText(generated!.envBlock, 'env')}>
                    {#if copiedField === 'env'}
                      <Check class="size-3.5" /> Copied
                    {:else}
                      <Copy class="size-3.5" /> Copy
                    {/if}
                  </Button>
                </div>
                <Textarea readonly value={generated.envBlock} rows={Math.min(10, generated.envBlock.split('\n').length)} class="font-mono text-body-sm bg-background" />
              </div>
            {/if}

            {#if generated.configContent}
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-caption-mono text-muted-foreground">
                    Config file
                    {#if generated.configPath}
                      <span class="text-muted-foreground/70"> · {generated.configPath}</span>
                    {/if}
                  </Label>
                  <Button variant="ghost" size="sm" class="h-7 gap-1.5 text-caption" onclick={() => copyText(generated!.configContent, 'config')}>
                    {#if copiedField === 'config'}
                      <Check class="size-3.5" /> Copied
                    {:else}
                      <Copy class="size-3.5" /> Copy
                    {/if}
                  </Button>
                </div>
                <Textarea readonly value={generated.configContent} rows={Math.min(14, generated.configContent.split('\n').length)} class="font-mono text-body-sm bg-background" />
              </div>
            {/if}

            {#if generated.runCommand}
              <div class="space-y-2">
                <Label class="text-caption-mono text-muted-foreground">Example command</Label>
                <div class="rounded-md border border-border bg-background px-3 py-2 font-mono text-body-sm">{generated.runCommand}</div>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    </ScrollArea>
  </Dialog.Content>
</Dialog.Root>

<!-- Reusable Model Picker Dialog -->
<ModelPickerDialog
  bind:open={modelPickerOpen}
  {models}
  selectedModel={modelPickerTarget === '_main' ? sel.model : (modelAliases[modelPickerTarget] || '')}
  onSelect={onModelPick}
/>
