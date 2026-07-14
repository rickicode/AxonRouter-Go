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
import { Switch } from '$lib/components/ui/switch';
import * as Select from '$lib/components/ui/select';
import { toast } from 'svelte-sonner';
import { copyToClipboard } from '$lib/utils';
import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
  import SearchIcon from '@lucide/svelte/icons/search';
  import InfoIcon from '@lucide/svelte/icons/info';
  import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
  import XCircleIcon from '@lucide/svelte/icons/x-circle';
  import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
  import { cliToolsApi, modelsApi, apiKeysApi } from '$lib/api';
  import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
  import type {
    CLITool,
    GatewayModel,
    APIKeyItem,
    CLIToolSelection,
    CLIToolConfig,
    CLIToolStatus,
    CLIToolState,
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
  let sel = $state<CLIToolSelection>({
    model: '',
    apiKeyId: '',
    baseUrl: '',
    models: [] as string[],
    useDiscovery: false,
    activeModel: '',
    subagentModel: '',
    agentModels: {},
  });
  let detailInstalled = $state(false);
  let detailHasRouter = $state(false);
  let detailState = $state<unknown>(null);
  let detailConfigured = $state(false);
  let detailConfig = $state<CLIToolConfig | null>(null);
  let generated = $state<CLIToolConfig | null>(null);
  let generating = $state(false);
  let resetting = $state(false);
  let copiedField = $state<string | null>(null);

  // Model alias mappings: { alias: gatewayModelId }
  let modelAliases = $state<Record<string, string>>({});

  // Model picker (reusable)
  let modelPickerOpen = $state(false);
  let modelPickerTarget = $state<string>('_main'); // '_main' or alias name
  let modelPickerMulti = $state(false);

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
    detailConfig = null;
    generating = false;
    resetting = false;
    copiedField = null;
    modelAliases = {};
    sel = {
      model: '',
      apiKeyId: '',
      baseUrl: defaultBaseUrl,
      models: [],
      useDiscovery: false,
      activeModel: '',
      subagentModel: '',
      agentModels: {},
    };
    try {
      const res: CLIToolState = await cliToolsApi.get(tool.id);
      const s = res.selection ?? ({} as CLIToolSelection);
      detailInstalled = res.installed ?? false;
      detailHasRouter = res.hasRouter ?? false;
      detailState = res.state ?? null;
      detailConfigured = res.configured ?? false;
      detailConfig = res.config ?? null;
      sel = {
        model: s.model ?? '',
        apiKeyId: s.apiKeyId ?? '',
        baseUrl: s.baseUrl || res.defaultBaseUrl || defaultBaseUrl,
        models: s.models ?? [],
        useDiscovery: s.useDiscovery ?? false,
        activeModel: s.activeModel ?? '',
        subagentModel: s.subagentModel ?? '',
        agentModels: s.agentModels ?? {},
      };
      // Restore saved model aliases
      if (s.modelAliases) {
        modelAliases = { ...s.modelAliases };
      }
      generated = res.config ?? null;
    } catch {
      detailInstalled = false;
      detailHasRouter = false;
      detailState = null;
      detailConfigured = false;
      detailConfig = null;
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

  async function applyConfig() {
    if (!selectedTool) return;
    generating = true;
    try {
      const res = await cliToolsApi.save(selectedTool.id, {
        ...sel,
        modelAliases: Object.keys(modelAliases).length > 0 ? modelAliases : undefined,
      });
      sel = res.selection;
      generated = res.config;
      detailConfig = res.config;
      detailConfigured = true;
      detailHasRouter = true;
      statuses[selectedTool.id] = {
        installed: detailInstalled,
        hasRouter: true,
        configured: true,
      };
      const path = res.config?.configPath;
      toast.success(`Generated config for ${selectedTool.name}${path ? ' → ' + path : ''}`);
      setTimeout(
        () => document.getElementById('generated-section')?.scrollIntoView({ behavior: 'smooth', block: 'start' }),
        50,
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to generate config');
    } finally {
      generating = false;
    }
  }

  async function resetConfig() {
    if (!selectedTool) return;
    resetting = true;
    try {
      await cliToolsApi.delete(selectedTool.id);
      generated = null;
      detailConfig = null;
      detailConfigured = false;
      detailHasRouter = false;
      statuses[selectedTool.id] = {
        installed: detailInstalled,
        hasRouter: false,
        configured: false,
      };
      toast.success(`Reset ${selectedTool.name} configuration`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to reset configuration');
    } finally {
      resetting = false;
    }
  }

async function copyText(text: string, field: string) {
	if (!text) return;
	try {
		await copyToClipboard(text);
		copiedField = field;
		toast.success('Copied to clipboard');
		setTimeout(() => (copiedField = null), 2000);
	} catch {
		toast.error('Failed to copy');
	}
}

  function openModelPicker(target: string, multi = false) {
    modelPickerTarget = target;
    modelPickerMulti = multi;
    modelPickerOpen = true;
  }

  function onModelPick(modelId: string) {
    if (modelPickerTarget === '_main') {
      sel.model = modelId;
    } else if (modelPickerTarget === 'activeModel') {
      sel.activeModel = modelId;
    } else if (modelPickerTarget === 'subagentModel') {
      sel.subagentModel = modelId;
    } else {
      modelAliases[modelPickerTarget] = modelId;
    }
  }

  function onMultiSelect(modelIds: string[]) {
    if (modelPickerTarget === '_main') {
      sel.models = modelIds;
    }
  }

  function addModel() {
    const m = sel.model.trim();
    if (!m) return;
    if (!sel.models?.includes(m)) {
      sel.models = [...(sel.models || []), m];
    }
    sel.model = '';
  }

  function removeModel(index: number) {
    sel.models = (sel.models || []).filter((_, i) => i !== index);
  }

  // Template variable substitution for code blocks and guide step values
  function replaceVars(text: string): string {
    const key = '__YOUR_AXONROUTER_API_KEY__';
    const base = sel.baseUrl
      ? sel.baseUrl.endsWith('/v1')
        ? sel.baseUrl
        : `${sel.baseUrl}/v1`
      : defaultBaseUrl;
    const model = sel.models?.[0] || sel.model || getFirstAliasModel() || 'provider/model-id';
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

  // Combined status badge (mirrors 9router's ToolSummaryCard):
  // Installed → Connected (has router) / Not configured (no router) ; Not installed otherwise.
  function getStatusBadge(toolId: string) {
    const s = statuses[toolId];
    if (!s || !s.installed) {
      return { label: 'Not installed', cls: 'bg-muted/40 text-muted-foreground border-border' };
    }
    if (s.hasRouter) {
      return { label: 'Connected', cls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' };
    }
    if (s.configured) {
      return { label: 'Configured', cls: 'bg-sky-500/10 text-sky-400 border-sky-500/20' };
    }
    return { label: 'Not configured', cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20' };
  }

  function getDetailBadge() {
    if (!detailInstalled) {
      return { label: 'Not installed', cls: 'bg-muted/40 text-muted-foreground border-border' };
    }
    if (detailHasRouter) {
      return { label: 'Connected', cls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' };
    }
    if (detailConfigured) {
      return { label: 'Configured', cls: 'bg-sky-500/10 text-sky-400 border-sky-500/20' };
    }
    return { label: 'Not configured', cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20' };
  }

  function getNoteIcon(type: string) {
    if (type === 'warning') return AlertTriangleIcon;
    if (type === 'error') return XCircleIcon;
    return InfoIcon;
  }

  function getNoteColors(type: string) {
    if (type === 'warning') return 'border-yellow-500/30 bg-yellow-500/10 text-yellow-400';
    if (type === 'error') return 'border-red-500/30 bg-red-500/10 text-red-400';
    return 'border-blue-500/30 bg-blue-500/10 text-blue-400';
  }

  // Tools that expose a subagent model slot in addition to the main model.
  function showsSubagentModel(tool: CLITool | null): boolean {
    return !!tool && (tool.id === 'codex' || tool.id === 'opencode' || tool.id === 'droid');
  }
  // Tools that expose an active model slot (primary default for multi-model tools).
  function showsActiveModel(tool: CLITool | null): boolean {
    return !!tool && (tool.id === 'opencode' || tool.id === 'droid');
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
            <img
              src={tool.image}
              alt={tool.name}
              class="size-8 rounded-lg object-contain"
              onerror={(e) => (e.currentTarget.style.display = 'none')}
            />
          </div>
          <div class="min-w-0 flex-1">
            <h3 class="truncate text-body-sm-strong">{tool.name}</h3>
            <span class="mt-1 inline-block rounded-full border px-1.5 py-0.5 text-[10px] font-medium {s.cls}"
              >{s.label}</span
            >
          </div>
          <ChevronRightIcon
            class="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5"
          />
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
            src={selectedTool.image}
            alt={selectedTool.name}
            class="size-8 rounded-lg object-contain"
            onerror={(e) => (e.currentTarget.style.display = 'none')}
          />
        {/if}
      </div>
      <div class="min-w-0 flex-1">
        <Dialog.Title class="text-body-md-strong">{selectedTool?.name}</Dialog.Title>
        <Dialog.Description class="text-caption text-muted-foreground"
          >{selectedTool?.description}</Dialog.Description
        >
      </div>
      <Badge variant="outline" class="border-0 {getDetailBadge().cls} shrink-0">{getDetailBadge().label}</Badge>
    </div>

    <!-- Body -->
    <ScrollArea class="flex-1 max-h-[60vh]">
      <div class="flex flex-col gap-4 p-4">
        <!-- Install / connection hint when tool is not detected locally -->
        {#if selectedTool && !detailInstalled}
          <div class="flex items-start gap-2.5 rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-yellow-400">
            <AlertTriangleIcon class="size-4 shrink-0 mt-0.5" />
            <p class="text-body-sm">
              {selectedTool.name} is not detected on this machine. You can still generate a manual config
              if AxonRouter runs on a remote server.
            </p>
          </div>
        {:else if selectedTool && detailInstalled && !detailHasRouter}
          <div class="flex items-start gap-2.5 rounded-lg border border-sky-500/30 bg-sky-500/10 p-3 text-sky-400">
            <InfoIcon class="size-4 shrink-0 mt-0.5" />
            <p class="text-body-sm">
              {selectedTool.name} is installed but not yet connected to AxonRouter. Apply a config to wire
              it up.
            </p>
          </div>
        {/if}

        <!-- Docs link -->
        {#if selectedTool?.docsUrl}
          <a
            href={selectedTool.docsUrl}
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1.5 text-caption text-muted-foreground hover:text-primary"
          >
            <ExternalLinkIcon class="size-3" /> Documentation
          </a>
        {/if}

        <!-- Shared: API Key (always shown; raw value resolved server-side from the selected key) -->
        <div class="space-y-2">
          <Label class="text-caption-mono uppercase text-muted-foreground">API Key</Label>
          <Select.Root type="single" value={sel.apiKeyId} onValueChange={(v: string) => (sel.apiKeyId = v)}>
            <Select.Trigger class="w-full h-10 text-body-sm">
              {@const selectedKey = keys.find((k) => k.id === sel.apiKeyId)}
              {selectedKey ? `${selectedKey.name || 'Untitled'} (${selectedKey.key_preview})` : '— Select API key —'}
            </Select.Trigger>
            <Select.Content>
              <Select.Item value="">— Select API key —</Select.Item>
              {#each keys as key}
                <Select.Item value={key.id}>{key.name || 'Untitled'} ({key.key_preview})</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
          <p class="text-caption text-muted-foreground">
            The real key is pulled from this selection automatically — no need to paste it.
          </p>
        </div>

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
                <div
                  class="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary text-[11px] font-bold"
                >
                  {step.step}
                </div>
                <div class="min-w-0 flex-1 space-y-1.5">
                  <p class="text-body-sm-strong">{step.title}</p>
                  {#if step.desc}
                    <p class="text-body-sm text-muted-foreground">{step.desc}</p>
                  {/if}
                  {#if step.value && step.copyable}
                    <code
                      class="block rounded-md border border-border bg-background px-2.5 py-1.5 font-mono text-body-sm"
                      >{replaceVars(step.value)}</code
                    >
                  {/if}
                  {#if step.type === 'modelSelector'}
                    <div class="space-y-3">
{#if selectedTool?.supportsDiscovery}
                    <div class="flex items-center gap-2">
                      <Switch id="auto-discovery" bind:checked={sel.useDiscovery} />
                      <Label for="auto-discovery" class="text-body-sm cursor-pointer">Auto-discover models from gateway</Label>
                    </div>
                  {/if}
                      {#if !sel.useDiscovery || !selectedTool?.supportsDiscovery}
                        <div class="flex flex-wrap gap-2">
                          {#each sel.models || [] as m, i (m)}
                            <div
                              class="flex items-center gap-1 rounded-md border border-border px-2 py-1 font-mono text-caption"
                            >
                              <span class="max-w-[200px] truncate">{m}</span>
                              <button
                                class="text-muted-foreground hover:text-foreground cursor-pointer"
                                onclick={() => removeModel(i)}><XCircleIcon class="size-3" /></button
                              >
                            </div>
                          {/each}
                        </div>
                        <div class="flex gap-2">
                          <Input
                            bind:value={sel.model}
                            placeholder="provider/model-id"
                            class="font-mono text-body-sm flex-1"
                          />
                          <Button
                            variant="outline"
                            size="sm"
                            class="gap-1.5"
                            onclick={() => openModelPicker('_main', true)}
                            disabled={models.length === 0}><SearchIcon class="size-3.5" /> Browse</Button
                          >
                          <Button
                            variant="outline"
                            size="sm"
                            onclick={addModel}
                            disabled={!sel.model?.trim()}>Add</Button
                          >
                        </div>
                      {/if}
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
            <Label class="text-caption-mono text-muted-foreground"
              >Config snippet ({selectedTool.codeBlock.language})</Label
            >
            <Textarea
              readonly
              value={replaceVars(selectedTool.codeBlock.code)}
              rows={Math.min(16, selectedTool.codeBlock.code.split('\n').length)}
              class="font-mono text-body-sm bg-background"
            />
          </div>
        {/if}

        <!-- MODEL ALIAS MAPPING UI (for tools with defaultModels) — 2-column grid -->
        {#if (selectedTool?.defaultModels?.length ?? 0) > 0}
          <div class="space-y-2">
            <Label class="text-caption-mono uppercase text-muted-foreground">Model aliases</Label>
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {#each selectedTool.defaultModels as dm}
                <button
                  type="button"
                  class="group flex flex-col gap-1 rounded-lg border border-border bg-background p-3 text-left transition-colors hover:border-primary/50 disabled:cursor-not-allowed disabled:opacity-50"
                  onclick={() => openModelPicker(dm.alias)}
                  disabled={models.length === 0}
                >
                  <span class="text-caption-mono text-muted-foreground">{dm.name}</span>
                  <span class="flex items-center justify-between gap-2">
                    <span class="min-w-0 flex-1 truncate font-mono text-body-sm">
                      {modelAliases[dm.alias] ?? dm.defaultValue ?? '— not set —'}
                    </span>
                    <SearchIcon class="size-3.5 shrink-0 text-muted-foreground group-hover:text-primary" />
                  </span>
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <!-- SINGLE MODEL UI (for simple tools without defaultModels / guideSteps) -->
        {#if (selectedTool?.guideSteps?.length ?? 0) === 0 && (selectedTool?.defaultModels?.length ?? 0) === 0}
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
                onclick={() => openModelPicker('_main')}
                disabled={models.length === 0}><SearchIcon class="size-3.5" /> Browse</Button
              >
            </div>
          </div>
        {/if}

        <!-- Extra model slots (active / subagent) for tool drivers that accept them -->
        {#if showsActiveModel(selectedTool)}
          <div class="space-y-2">
            <Label class="text-caption-mono uppercase text-muted-foreground">Active model</Label>
            <div class="flex gap-2">
              <Input
                bind:value={sel.activeModel}
                placeholder="provider/model-id (default when empty)"
                class="font-mono text-body-sm flex-1"
              />
              <Button
                variant="outline"
                size="sm"
                class="shrink-0 gap-1.5"
                onclick={() => openModelPicker('activeModel')}
                disabled={models.length === 0}><SearchIcon class="size-3.5" /> Browse</Button
              >
            </div>
          </div>
        {/if}
        {#if showsSubagentModel(selectedTool)}
          <div class="space-y-2">
            <Label class="text-caption-mono uppercase text-muted-foreground">Subagent model</Label>
            <div class="flex gap-2">
              <Input
                bind:value={sel.subagentModel}
                placeholder="provider/model-id (defaults to main model)"
                class="font-mono text-body-sm flex-1"
              />
              <Button
                variant="outline"
                size="sm"
                class="shrink-0 gap-1.5"
                onclick={() => openModelPicker('subagentModel')}
                disabled={models.length === 0}><SearchIcon class="size-3.5" /> Browse</Button
              >
            </div>
          </div>
        {/if}

        <!-- Shared: Base URL (render once for every tool) -->
        <div class="space-y-2">
          <Label class="text-caption-mono uppercase text-muted-foreground">Gateway Base URL</Label>
          <Input bind:value={sel.baseUrl} placeholder={defaultBaseUrl} class="font-mono text-body-sm" />
        </div>

        <!-- Generated config output -->
        {#if generated}
          <div id="generated-section" class="mt-2 space-y-4 rounded-lg border border-border bg-background/50 p-4">
            <h3 class="text-body-sm-strong">Generated config</h3>
            {#if generated.envBlock}
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-caption-mono text-muted-foreground">Environment variables</Label>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-7 gap-1.5 text-caption"
                    onclick={() => copyText(generated!.envBlock, 'env')}
                  >
                    {#if copiedField === 'env'}
                      <Check class="size-3.5" /> Copied
                    {:else}
                      <Copy class="size-3.5" /> Copy
                    {/if}
                  </Button>
                </div>
                <Textarea
                  readonly
                  value={generated.envBlock}
                  rows={Math.min(10, generated.envBlock.split('\n').length)}
                  class="font-mono text-body-sm bg-background"
                />
              </div>
            {/if}
            {#if generated.configContent}
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-caption-mono text-muted-foreground">
                    Config file {#if generated.configPath}
                      <span class="text-muted-foreground/70"> · {generated.configPath}</span>
                    {/if}
                  </Label>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="h-7 gap-1.5 text-caption"
                    onclick={() => copyText(generated!.configContent, 'config')}
                  >
                    {#if copiedField === 'config'}
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
                {#if generated.backupPath}
                  <p class="mt-1 text-caption text-muted-foreground">
                    Backup tersimpan di: <code class="font-mono">{generated.backupPath}</code>
                  </p>
                {/if}
              </div>
            {/if}
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

    <!-- Sticky footer: Apply / Reset (always visible, outside scroll) -->
    <div class="flex items-center gap-2 border-t border-border p-4">
      <Button
        variant="default"
        class="flex-1 cursor-pointer"
        onclick={applyConfig}
        disabled={generating}
      >
        {generating ? 'Generating…' : 'Generate config'}
      </Button>
      <Button
        variant="outline"
        class="cursor-pointer gap-1.5"
        onclick={resetConfig}
        disabled={resetting || !detailConfigured}
      >
        <RotateCcwIcon class="size-3.5" />
        {resetting ? 'Resetting…' : 'Reset'}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>

<!-- Reusable Model Picker Dialog -->
<ModelPickerDialog
  bind:open={modelPickerOpen}
  {models}
  selectedModel={modelPickerTarget === '_main'
    ? sel.model
    : modelPickerTarget === 'activeModel'
      ? sel.activeModel ?? ''
      : modelPickerTarget === 'subagentModel'
        ? sel.subagentModel ?? ''
        : (modelAliases[modelPickerTarget] || '')}
  selectedModels={sel.models}
  onSelect={onModelPick}
  onMultiSelect={onMultiSelect}
  multi={modelPickerMulti}
/>
