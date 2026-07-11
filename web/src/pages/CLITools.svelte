<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Textarea } from '$lib/components/ui/textarea';
  import * as Select from '$lib/components/ui/select';
  import { toast } from 'svelte-sonner';
  import { Bot, SquareTerminal, Sparkles, Boxes, Orbit, Terminal, Copy, Check, ExternalLink } from '@lucide/svelte';
  import { cliToolsApi, modelsApi, apiKeysApi } from '$lib/api';
  import type { CLITool, GatewayModel, APIKeyItem, CLIToolSelection, CLIToolConfig } from '$lib/api';

  type ToolState = {
    selection: CLIToolSelection;
    generated: CLIToolConfig | null;
    apiKeyValue: string;
    generating: boolean;
    copiedEnv: boolean;
    copiedConfig: boolean;
  };

  const iconMap: Record<string, typeof Bot> = {
    claude: Bot,
    codex: SquareTerminal,
    cline: Sparkles,
    kilo: Boxes,
    openclaw: Orbit,
    generic: Terminal,
  };

  let tools = $state<CLITool[]>([]);
  let models = $state<GatewayModel[]>([]);
  let keys = $state<APIKeyItem[]>([]);
  let states = $state<Record<string, ToolState>>({});
  let loading = $state(true);
  let pageError = $state<string | null>(null);

  const defaultBaseUrl = typeof window !== 'undefined' ? `${window.location.origin}/v1` : 'http://localhost:3777/v1';

  onMount(() => {
    document.title = 'CLI Tools — AxonRouter';
    loadAll();
  });

  async function loadAll() {
    loading = true;
    pageError = null;
    try {
      const [toolsRes, modelsRes, keysRes] = await Promise.all([
        cliToolsApi.list(),
        modelsApi.list(),
        apiKeysApi.list(),
      ]);
      tools = toolsRes.data ?? [];
      models = modelsRes.data ?? [];
      keys = keysRes.data ?? [];

      const initial: Record<string, ToolState> = {};
      for (const tool of tools) {
        initial[tool.id] = {
          selection: { model: '', apiKeyId: '', baseUrl: defaultBaseUrl },
          generated: null,
          apiKeyValue: '',
          generating: false,
          copiedEnv: false,
          copiedConfig: false,
        };
      }
      states = initial;

      await Promise.all(tools.map((t) => loadToolState(t.id)));
    } catch (err) {
      pageError = err instanceof Error ? err.message : 'Failed to load CLI tools';
      toast.error(pageError);
    } finally {
      loading = false;
    }
  }

  async function loadToolState(toolId: string) {
    try {
      const res = await cliToolsApi.get(toolId);
      const sel = res.selection;
      states[toolId] = {
        ...states[toolId],
        selection: {
          model: sel.model ?? '',
          apiKeyId: sel.apiKeyId ?? '',
          baseUrl: sel.baseUrl || res.defaultBaseUrl || defaultBaseUrl,
        },
      };
    } catch {
      // No saved state yet; keep defaults
    }
  }

  async function generateConfig(toolId: string) {
    const state = states[toolId];
    if (!state) return;
    if (!state.selection.model) {
      toast.error('Please select a model');
      return;
    }
    state.generating = true;
    try {
      const res = await cliToolsApi.save(toolId, {
        ...state.selection,
        apiKeyValue: state.apiKeyValue,
      });
      states[toolId] = {
        ...state,
        selection: res.selection,
        generated: res.config,
      };
      toast.success('Config generated');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to generate config');
    } finally {
      state.generating = false;
    }
  }

  async function copyText(text: string, toolId: string, field: 'copiedEnv' | 'copiedConfig') {
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
      states[toolId][field] = true;
      toast.success('Copied to clipboard');
      setTimeout(() => {
        states[toolId][field] = false;
      }, 2000);
    } catch {
      toast.error('Failed to copy');
    }
  }

  function selectedModelLabel(toolId: string): string {
    const modelId = states[toolId]?.selection.model;
    if (!modelId) return 'Select model';
    const m = models.find((x) => x.id === modelId);
    return m ? `${m.id}` : modelId;
  }

  function selectedKeyLabel(toolId: string): string {
    const keyId = states[toolId]?.selection.apiKeyId;
    if (!keyId) return 'Select API key';
    const k = keys.find((x) => x.id === keyId);
    return k ? `${k.name || 'Untitled'} (${k.key_preview})` : keyId;
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

  <div class="grid grid-cols-1 gap-6">
    {#each tools as tool (tool.id)}
      {@const state = states[tool.id]}
      {@const Icon = iconMap[tool.id] ?? Terminal}
      {#if state}
        <Card class="bg-card border-border shadow-card rounded-xl overflow-hidden">
          <CardHeader class="pb-4">
            <div class="flex items-start justify-between gap-4">
              <div class="flex items-center gap-3">
                <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                  <Icon class="size-5" />
                </div>
                <div>
                  <CardTitle class="text-display-md">{tool.name}</CardTitle>
                  <CardDescription class="text-body-sm text-muted-foreground">
                    {tool.description}
                  </CardDescription>
                </div>
              </div>
              {#if tool.docsUrl}
                <Button variant="ghost" size="sm" class="shrink-0 gap-1.5 text-caption" href={tool.docsUrl} target="_blank" rel="noopener noreferrer">
                  Docs
                  <ExternalLink class="size-3.5" />
                </Button>
              {/if}
            </div>
          </CardHeader>

          <CardContent class="space-y-6">
            <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
              <!-- Model -->
              <div class="space-y-2">
                <Label class="text-caption-mono uppercase text-muted-foreground">Model</Label>
                <Select.Root type="single" bind:value={state.selection.model}>
                  <Select.Trigger class="w-full">
                    {selectedModelLabel(tool.id)}
                  </Select.Trigger>
                  <Select.Content class="max-h-72 overflow-auto">
                    {#each models as model}
                      <Select.Item value={model.id}>{model.id}</Select.Item>
                    {/each}
                  </Select.Content>
                </Select.Root>
              </div>

              <!-- API Key -->
              <div class="space-y-2">
                <Label class="text-caption-mono uppercase text-muted-foreground">API Key</Label>
                <Select.Root type="single" bind:value={state.selection.apiKeyId}>
                  <Select.Trigger class="w-full">
                    {selectedKeyLabel(tool.id)}
                  </Select.Trigger>
                  <Select.Content>
                    {#each keys as key}
                      <Select.Item value={key.id}>{key.name || 'Untitled'} ({key.key_preview})</Select.Item>
                    {/each}
                    {#if keys.length === 0}
                      <div class="px-2 py-1.5 text-caption text-muted-foreground">No API keys yet</div>
                    {/if}
                  </Select.Content>
                </Select.Root>
              </div>

              <!-- Base URL -->
              <div class="space-y-2">
                <Label class="text-caption-mono uppercase text-muted-foreground">Gateway Base URL</Label>
                <Input bind:value={state.selection.baseUrl} placeholder={defaultBaseUrl} class="font-mono text-body-sm" />
              </div>
            </div>

            <!-- API Key Value -->
            <div class="space-y-2">
              <Label class="text-caption-mono uppercase text-muted-foreground">Raw API Key Value</Label>
              <Input
                type="password"
                bind:value={state.apiKeyValue}
                placeholder="Paste your AxonRouter API key value here (not stored)"
                class="text-body-sm"
              />
              <p class="text-caption text-muted-foreground">
                AxonRouter stores only bcrypt hashes of API keys, so the raw value cannot be retrieved. Paste it once to embed it in the generated config.
              </p>
            </div>

            <div class="flex justify-end">
              <Button
                variant="outline"
                size="sm"
                class="text-body-sm rounded-sm cursor-pointer"
                onclick={() => generateConfig(tool.id)}
                disabled={state.generating || !state.selection.model}
              >
                {state.generating ? 'Generating…' : 'Generate config'}
              </Button>
            </div>

            {#if state.generated}
              <div class="space-y-4 rounded-lg border border-border bg-background/50 p-4">
                <div class="flex items-center justify-between">
                  <h3 class="text-body-sm-strong">Generated config</h3>
                </div>

                <!-- Env block -->
                <div class="space-y-2">
                  <div class="flex items-center justify-between">
                    <Label class="text-caption-mono text-muted-foreground">Environment variables</Label>
                    <Button
                      variant="ghost"
                      size="sm"
                      class="h-7 gap-1.5 text-caption"
                      onclick={() => copyText(state.generated.envBlock, tool.id, 'copiedEnv')}
                    >
                      {#if state.copiedEnv}
                        <Check class="size-3.5" />
                        Copied
                      {:else}
                        <Copy class="size-3.5" />
                        Copy
                      {/if}
                    </Button>
                  </div>
                  <Textarea readonly value={state.generated.envBlock} rows={Math.min(8, state.generated.envBlock.split('\n').length)} class="font-mono text-body-sm bg-background" />
                </div>

                <!-- Config file -->
                {#if state.generated.configContent}
                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                      <Label class="text-caption-mono text-muted-foreground">
                        Config file {#if state.generated.configPath}<span class="text-muted-foreground/70">· {state.generated.configPath}</span>{/if}
                      </Label>
                      <Button
                        variant="ghost"
                        size="sm"
                        class="h-7 gap-1.5 text-caption"
                        onclick={() => copyText(state.generated.configContent, tool.id, 'copiedConfig')}
                      >
                        {#if state.copiedConfig}
                          <Check class="size-3.5" />
                          Copied
                        {:else}
                          <Copy class="size-3.5" />
                          Copy
                        {/if}
                      </Button>
                    </div>
                    <Textarea readonly value={state.generated.configContent} rows={Math.min(12, state.generated.configContent.split('\n').length)} class="font-mono text-body-sm bg-background" />
                  </div>
                {/if}

                <!-- Run command -->
                {#if state.generated.runCommand}
                  <div class="space-y-2">
                    <Label class="text-caption-mono text-muted-foreground">Example command</Label>
                    <div class="rounded-md border border-border bg-background px-3 py-2 font-mono text-body-sm">
                      {state.generated.runCommand}
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          </CardContent>
        </Card>
      {/if}
    {/each}
  </div>

  {#if loading}
    <div class="text-body-sm text-muted-foreground">Loading CLI tools…</div>
  {:else if tools.length === 0}
    <div class="text-body-sm text-muted-foreground">No CLI tools configured.</div>
  {/if}
</div>
