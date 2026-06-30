<script lang="ts">
  import { PROVIDER_CATALOG, CATEGORIES, getProviderMeta } from '$lib/provider-catalog';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';
  import { connectionsApi } from '$lib/api';
  import { Card, CardContent } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { onMount } from 'svelte';

  let step = $state<'select' | 'configure' | 'done'>('select');
  let selectedProvider = $state<string | null>(null);
  let searchQuery = $state('');
  let activeCategory = $state('');

  let connectionName = $state('');
  let apiKey = $state('');
  let loading = $state(false);
  let resultMsg = $state('');
  let resultOk = $state(false);

  let meta = $derived(selectedProvider ? getProviderMeta(selectedProvider) : null);

  let filteredCatalog = $derived.by(() => {
    let list = PROVIDER_CATALOG;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      list = list.filter(p =>
        p.displayName.toLowerCase().includes(q) ||
        p.id.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q)
      );
    }
    if (activeCategory) {
      list = list.filter(p => p.category === activeCategory);
    }
    return list;
  });

  onMount(() => { document.title = 'Add Provider - AxonRouter'; });

  function selectProvider(id: string) {
    selectedProvider = id;
    connectionName = '';
    apiKey = '';
    resultMsg = '';
    step = 'configure';
  }

  async function handleAddConnection() {
    if (!selectedProvider) return;
    loading = true;
    resultMsg = '';
    try {
      const name = connectionName.trim() || `${selectedProvider}-key-001`;
      const data: Record<string, unknown> = { name };
      if (meta?.authType === 'apikey' && apiKey.trim()) {
        data.api_key = apiKey.trim();
      }
      await connectionsApi.create(selectedProvider, data);
      resultOk = true;
      resultMsg = `Connection "${name}" added successfully!`;
      step = 'done';
    } catch (err) {
      resultOk = false;
      resultMsg = err instanceof Error ? err.message : 'Failed to add connection';
    } finally {
      loading = false;
    }
  }

  const filterCategories = CATEGORIES.filter(c => ['oauth', 'apikey', 'free', 'free_tier', 'compatible'].includes(c.id));
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="flex items-center gap-2 text-body-sm text-muted-foreground">
    <a href="/providers" class="hover:text-foreground transition-colors">Providers</a>
    <span>/</span>
    <span class="text-foreground">Add provider</span>
  </div>

  {#if step === 'select'}
    <div class="space-y-1">
      <h1 class="text-display-lg">Add provider.</h1>
      <p class="text-body-sm text-muted-foreground">
        Select a provider to add a new connection. Each connection represents a single API key or OAuth credential.
      </p>
    </div>

    <div class="flex flex-col gap-3">
      <Input
        type="text"
        class="h-9 max-w-sm text-body-sm"
        placeholder="Search providers..."
        bind:value={searchQuery}
      />
      <div class="flex flex-wrap gap-2">
        <button
          class="rounded-full border px-3 py-1 text-caption-mono transition-colors
            {!activeCategory ? 'border-foreground bg-foreground text-background' : 'border-border text-muted-foreground hover:border-foreground hover:text-foreground'}"
          onclick={() => activeCategory = ''}
        >
          All
        </button>
        {#each filterCategories as cat}
          <button
            class="rounded-full border px-3 py-1 text-caption-mono transition-colors
              {activeCategory === cat.id ? 'border-foreground bg-foreground text-background' : 'border-border text-muted-foreground hover:border-foreground hover:text-foreground'}"
            onclick={() => activeCategory = activeCategory === cat.id ? '' : cat.id}
          >
            {cat.label}
          </button>
        {/each}
      </div>
    </div>

    {#if filteredCatalog.length > 0}
      <div class="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
        {#each filteredCatalog as provider}
          <button
            class="group flex items-center gap-3 p-4 rounded-lg border border-border bg-card text-left transition-all hover:border-foreground/20 hover:bg-accent/10 cursor-pointer"
            onclick={() => selectProvider(provider.id)}
          >
            <div
              class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md overflow-hidden"
              style="background: {provider.color}15;"
            >
              <ProviderIcon meta={provider} size={40} />
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-body-sm font-semibold text-foreground truncate">{provider.displayName}</span>
                <span class="size-2 rounded-full shrink-0" style="background: {CATEGORIES.find(c => c.id === provider.category)?.color ?? '#888'}"></span>
              </div>
              <p class="text-caption-mono text-muted-foreground truncate">{provider.description}</p>
              <div class="flex gap-1.5 mt-1.5">
                <Badge variant="outline" class="text-caption-mono rounded-full py-0">{provider.prefix}</Badge>
                <Badge variant="outline" class="text-caption-mono rounded-full py-0">{provider.format}</Badge>
                {#if provider.authType === 'none'}
                  <Badge variant="secondary" class="text-caption-mono rounded-full py-0">Free</Badge>
                {:else if provider.authType === 'oauth'}
                  <Badge variant="secondary" class="text-caption-mono rounded-full py-0">OAuth</Badge>
                {:else}
                  <Badge variant="secondary" class="text-caption-mono rounded-full py-0">API Key</Badge>
                {/if}
              </div>
            </div>
          </button>
        {/each}
      </div>
    {:else}
      <Card class="shadow-vercel-2 border">
        <CardContent class="flex flex-col items-center justify-center py-16">
          <p class="text-body-sm text-muted-foreground">No providers match your search.</p>
        </CardContent>
      </Card>
    {/if}

  {:else if step === 'configure' && meta}
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        <button class="text-muted-foreground hover:text-foreground transition-colors" onclick={() => step = 'select'} aria-label="Back to provider selection">
          <svg class="size-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
        </button>
        <div
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md overflow-hidden"
          style="background: {meta.color}15;"
        >
          <ProviderIcon {meta} size={40} />
        </div>
        <div>
          <h1 class="text-display-md">Add {meta.displayName} connection.</h1>
          <p class="text-caption-mono text-muted-foreground">{meta.description}</p>
        </div>
      </div>
    </div>

    <Card class="shadow-vercel-2 border max-w-xl">
      <CardContent class="pt-6 space-y-4">
        <div class="space-y-2">
          <Label class="text-body-sm font-medium">Connection name</Label>
          <Input bind:value={connectionName} placeholder="{meta.id}-key-001" class="h-10 text-body-sm" />
          <p class="text-caption-mono text-muted-foreground">A friendly name to identify this connection.</p>
        </div>

        {#if meta.authType === 'apikey'}
          <div class="space-y-2">
            <Label class="text-body-sm font-medium">API key</Label>
            <Input bind:value={apiKey} type="password" placeholder="sk-..." class="h-10 text-body-sm font-mono" />
            <p class="text-caption-mono text-muted-foreground">Your {meta.displayName} API key. It will be stored encrypted in SQLite.</p>
          </div>
        {:else if meta.authType === 'oauth'}
          <div class="p-4 rounded-md bg-accent/50 border border-border">
            <p class="text-body-sm text-muted-foreground">
              This provider uses OAuth authentication. After creating the connection, you'll be redirected to authorize access.
            </p>
          </div>
        {:else if meta.authType === 'none'}
          <div class="p-4 rounded-md bg-accent/50 border border-border">
            <p class="text-body-sm text-muted-foreground">
              This provider is free and requires no authentication. The connection will be activated automatically.
            </p>
          </div>
        {/if}

        {#if resultMsg}
          <p class="text-body-sm {resultOk ? 'text-emerald-500' : 'text-destructive'}">{resultMsg}</p>
        {/if}

        <div class="flex gap-3 pt-2">
          <Button onclick={handleAddConnection} disabled={loading} class="text-body-sm">
            {loading ? 'Adding...' : 'Add connection'}
          </Button>
          <Button onclick={() => step = 'select'} variant="ghost" class="text-body-sm">Back</Button>
        </div>
      </CardContent>
    </Card>

  {:else if step === 'done'}
    <Card class="shadow-vercel-2 border max-w-xl">
      <CardContent class="flex flex-col items-center justify-center py-16 text-center">
        <div class="size-12 rounded-full bg-emerald-500/10 flex items-center justify-center mb-4">
          <svg class="size-6 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <h3 class="text-body-md font-semibold mb-1">Connection added!</h3>
        <p class="text-body-sm text-muted-foreground mb-6">{resultMsg}</p>
        <div class="flex gap-3">
          <Button onclick={() => { step = 'select'; selectedProvider = null; resultMsg = ''; }} variant="outline" class="text-body-sm">
            Add another
          </Button>
          <a href="/providers/{selectedProvider}" class="inline-flex items-center justify-center h-9 px-4 text-body-sm bg-foreground text-background rounded-md hover:bg-foreground/90 transition-colors">
            View provider
          </a>
        </div>
      </CardContent>
    </Card>
  {/if}
</div>
