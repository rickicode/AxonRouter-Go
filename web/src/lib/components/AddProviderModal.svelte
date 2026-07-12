<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { providersApi, type Provider } from '$lib/api';
import { toast } from 'svelte-sonner';
  import { resolveProviderCatalogId } from '$lib/provider-catalog';

  let { open = $bindable(false), onCreated }: { open: boolean; onCreated?: (provider: Provider) => void } = $props();

  let mode = $state<'openai' | 'anthropic'>('openai');
  let name = $state('');
  let baseUrl = $state('');
  let apiKey = $state('');
  let submitting = $state(false);
  let error = $state('');
  let step = $state<'form' | 'done'>('form');
  let createdProvider = $state<Provider | null>(null);

  const MODE_DEFAULTS = {
    openai: { format: 'openai', placeholder: 'https://api.example.com/v1', label: 'OpenAI-compatible' },
    anthropic: { format: 'anthropic', placeholder: 'https://api.example.com/v1', label: 'Anthropic-compatible' },
  };

  function reset() {
    name = '';
    baseUrl = '';
    apiKey = '';
    error = '';
    step = 'form';
    createdProvider = null;
    submitting = false;
  }

  function handleOpenChange(isOpen: boolean) {
    if (!isOpen) reset();
    open = isOpen;
  }

  // Derive a URL-safe id from the provider name
  function deriveId(displayName: string): string {
    return displayName
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '')
      .slice(0, 40);
  }

  async function handleSubmit() {
    error = '';
    if (!name.trim()) { error = 'Provider name is required'; return; }
    if (!baseUrl.trim()) { error = 'Base URL is required'; return; }

    const id = deriveId(name.trim());
    if (!id) { error = 'Invalid provider name'; return; }

    // Check if id conflicts with existing catalog provider
    const resolved = resolveProviderCatalogId(id);
    if (resolved !== id) {
      error = `Name "${id}" conflicts with a built-in provider. Choose a different name.`;
      return;
    }

    submitting = true;
    try {
      const format = MODE_DEFAULTS[mode].format;
      const result = await providersApi.create({
        name: id,
        format,
        base_url: baseUrl.trim(),
        display_name: name.trim(),
        is_custom: true,
      });
		createdProvider = result;
		step = 'done';
		toast.success(`Provider "${name}" created`);
		onCreated?.(result);
	} catch (e) {
		error = e instanceof Error ? e.message : 'Failed to create provider';
		toast.error(error);
	} finally {
      submitting = false;
    }
  }
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
  <Dialog.Content class="sm:max-w-[480px]">
    {#if step === 'form'}
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">Add custom provider</Dialog.Title>
        <Dialog.Description class="text-sm text-muted-foreground">
          Connect an OpenAI or Anthropic compatible API endpoint that isn't in the built-in list.
        </Dialog.Description>
      </Dialog.Header>

      <div class="flex flex-col gap-4 py-2">
        <!-- Mode toggle -->
        <div class="flex gap-2">
          {#each (['openai', 'anthropic'] as const) as m}
            <button
              type="button"
              class="flex-1 rounded-lg border px-3 py-2 text-sm font-medium transition-colors
                {mode === m
                  ? 'border-foreground bg-foreground text-background'
                  : 'border-border bg-background text-muted-foreground hover:border-foreground/30 hover:text-foreground'}"
              onclick={() => { mode = m; error = ''; }}
            >
              {MODE_DEFAULTS[m].label}
            </button>
          {/each}
        </div>

        <!-- Name -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Provider name</Label>
          <Input
            bind:value={name}
            placeholder="My Custom LLM"
            class="h-9 text-sm"
          />
          {#if name.trim()}
            <p class="text-[11px] text-muted-foreground">ID: <code class="font-mono">{deriveId(name.trim())}</code></p>
          {/if}
        </div>

        <!-- Base URL -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Base URL</Label>
          <Input
            bind:value={baseUrl}
            placeholder={MODE_DEFAULTS[mode].placeholder}
            class="h-9 text-sm font-mono"
          />
          <p class="text-[11px] text-muted-foreground">
            {#if mode === 'openai'}
              The /v1 endpoint. Requests go to {baseUrl.trim() || '...'}/chat/completions
            {:else}
              The /v1 endpoint. Requests go to {baseUrl.trim() || '...'}/messages
            {/if}
          </p>
        </div>

        <!-- API Key (optional) -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">API key <span class="text-muted-foreground font-normal">(optional, can add later)</span></Label>
          <Input
            bind:value={apiKey}
            type="password"
            placeholder="sk-..."
            class="h-9 text-sm font-mono"
          />
        </div>

        {#if error}
          <p class="rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-sm text-destructive">{error}</p>
        {/if}
      </div>

      <Dialog.Footer>
        <Button variant="outline" onclick={() => handleOpenChange(false)} class="text-sm">Cancel</Button>
        <Button onclick={handleSubmit} disabled={submitting} class="text-sm">
          {submitting ? 'Creating...' : 'Create provider'}
        </Button>
      </Dialog.Footer>

    {:else}
      <!-- Success -->
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">Provider created</Dialog.Title>
      </Dialog.Header>

      <div class="flex flex-col items-center gap-3 py-4">
        <div class="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-500/10">
          <svg class="h-6 w-6 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <div class="text-center">
          <p class="font-medium">{createdProvider?.display_name ?? name}</p>
          <p class="text-sm text-muted-foreground">
            Provider <code class="font-mono">{createdProvider?.id}</code> is ready. Add a connection with an API key to start routing.
          </p>
        </div>
      </div>

      <Dialog.Footer>
        <Button variant="outline" onclick={() => { reset(); handleOpenChange(false); }} class="text-sm">Done</Button>
        <Button onclick={() => { reset(); handleOpenChange(false); window.location.href = `/providers/${createdProvider?.id}`; }} class="text-sm">
          Go to provider
        </Button>
      </Dialog.Footer>
    {/if}
  </Dialog.Content>
</Dialog.Root>
