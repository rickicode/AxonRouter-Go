<script lang="ts">
  import { PROVIDER_CATALOG, CATEGORIES, getProviderMeta } from '$lib/provider-catalog';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';
  import { connectionsApi, proxyPoolsApi, type CreateConnectionPayload, type ProxyPool } from '$lib/api';
  import { Card, CardContent } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
import * as Select from '$lib/components/ui/select';
  import { onMount } from 'svelte';
import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
import { toast } from 'svelte-sonner';

  let step = $state<'select' | 'configure' | 'done'>('select');
  let selectedProvider = $state<string | null>(null);
  let searchQuery = $state('');
  let activeCategory = $state('');

let connectionName = $state('');
let apiKey = $state('');
let selectedRegion = $state('');
let noAuthMode = $state<'direct' | 'http' | 'relay'>('direct');
let selectedProxyPoolId = $state('');
  let proxyPools = $state<ProxyPool[]>([]);
  let loadingPools = $state(false);
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

  onMount(() => { document.title = 'Add Provider — AxonRouter'; });

function selectProvider(id: string) {
  selectedProvider = id;
  connectionName = '';
  apiKey = '';
  const meta = getProviderMeta(id);
  selectedRegion = meta?.defaultRegion ?? '';
  noAuthMode = 'direct';
  selectedProxyPoolId = '';
  resultMsg = '';
  step = 'configure';
  if (meta?.authType === 'none') {
    loadProxyPools();
  }
}

async function loadProxyPools() {
	loadingPools = true;
	try {
    const res = await proxyPoolsApi.list({ is_active: '1', per_page: '200' });
    proxyPools = res.data ?? [];
	} catch {
		proxyPools = [];
	} finally {
		loadingPools = false;
	}
}

const httpProxyPools = $derived(proxyPools.filter(p => p.type === 'http' || p.type === 'https'));
const relayProxyPools = $derived(proxyPools.filter(p => p.type === 'relay' || p.type === 'vercel' || p.type === 'deno' || p.type === 'cloudflare'));
const availableProxyPools = $derived(noAuthMode === 'http' ? httpProxyPools : relayProxyPools);

  async function handleAddConnection() {
    if (!selectedProvider) return;
    loading = true;
    resultMsg = '';
    try {
    const name = connectionName.trim() || `${selectedProvider}-key-001`;
    const data: CreateConnectionPayload = { name };
    if (meta?.authType === 'none') {
      data.auth_type = 'none';
      if (noAuthMode !== 'direct' && selectedProxyPoolId) {
        data.provider_specific_data = { proxyPoolId: selectedProxyPoolId };
      }
    } else if (meta?.authType === 'apikey' && apiKey.trim()) {
      data.api_key = apiKey.trim();
      if (selectedRegion) {
        data.provider_specific_data = { ...(data.provider_specific_data ?? {}), region: selectedRegion };
      }
    }
    await connectionsApi.create(selectedProvider, data);
			resultOk = true;
			resultMsg = `Connection "${name}" added successfully!`;
			step = 'done';
			toast.success(`Connection "${name}" added`);
	} catch (err) {
		resultOk = false;
		resultMsg = '';
		toast.error(err instanceof Error ? err.message : 'Failed to add connection');
	} finally {
      loading = false;
    }
  }

	const filterCategories = CATEGORIES.filter(c => ['oauth', 'apikey', 'service-account', 'free', 'free_tier', 'compatible'].includes(c.id));

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
      <p class="text-body-lg text-muted-foreground">
        Select a provider to add a new connection. Each connection represents a single API key or OAuth credential.
      </p>
    </div>

    <!-- Filter bar — DESIGN.md tab-ghost pills -->
    <div class="flex flex-col gap-3">
      <Input
        type="text"
        class="h-10 max-w-sm text-body-sm"
        placeholder="Search providers..."
        bind:value={searchQuery}
      />
      <div class="flex flex-wrap gap-2">
        <button
          class="rounded-sm px-4 py-1.5 text-body-sm transition-colors
            {!activeCategory ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
          onclick={() => activeCategory = ''}
        >
          All
        </button>
        {#each filterCategories as cat}
          <button
            class="rounded-sm px-4 py-1.5 text-body-sm transition-colors
              {activeCategory === cat.id ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
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
            class="group flex items-center gap-3 p-4 rounded-lg shadow-card bg-card text-left transition-all hover:border-foreground/20 hover:bg-accent/10 cursor-pointer"
            onclick={() => selectProvider(provider.id)}
          >
            <div
              class="flex size-10 shrink-0 items-center justify-center rounded-md overflow-hidden"
              style="background: {provider.color}15;"
            >
              <ProviderIcon meta={provider} size={40} />
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-body-sm-strong text-foreground truncate">{provider.displayName}</span>
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
      <Card class="shadow-card">
        <CardContent class="flex flex-col items-center justify-center py-16">
          <p class="text-body-sm text-muted-foreground">No providers match your search.</p>
        </CardContent>
      </Card>
    {/if}

  {:else if step === 'configure' && meta}
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        <button class="text-muted-foreground hover:text-foreground transition-colors" onclick={() => step = 'select'} aria-label="Back to provider selection">
          <ArrowLeftIcon class="size-5" />
        </button>
        <div
          class="flex size-10 shrink-0 items-center justify-center rounded-md overflow-hidden"
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

    <Card class="shadow-card max-w-xl">
      <CardContent class="pt-6 space-y-4">
        <div class="space-y-2">
          <Label class="text-body-sm-strong">Connection name</Label>
          <Input bind:value={connectionName} placeholder="{meta.id}-key-001" class="h-10 text-body-sm" />
          <p class="text-caption text-muted-foreground">A friendly name to identify this connection.</p>
        </div>

  {#if meta.authType === 'apikey'}
  <div class="space-y-2">
    <Label class="text-body-sm-strong">API key</Label>
    <Input bind:value={apiKey} type="password" placeholder="sk-..." class="h-10 text-code font-mono" />
    <p class="text-caption text-muted-foreground">Your {meta.displayName} API key. Stored encrypted in SQLite.</p>
  </div>

  {#if meta.regionOptions?.length}
  <div class="space-y-2">
    <Label class="text-body-sm-strong">Region</Label>
    <Select.Root type="single" value={selectedRegion} onValueChange={(v: string) => (selectedRegion = v)}>
      <Select.Trigger class="h-10 w-full text-body-sm">
        {selectedRegion || 'Select a region…'}
      </Select.Trigger>
      <Select.Content>
        <Select.Item value="">Select a region…</Select.Item>
        {#each meta.regionOptions as region}
          <Select.Item value={region}>{region}</Select.Item>
        {/each}
      </Select.Content>
    </Select.Root>
    <p class="text-caption text-muted-foreground">Bedrock API keys are region-specific. Pick the AWS region where the key was created.</p>
  </div>
  {/if}
  {:else if meta.authType === 'oauth'}
          <div class="p-4 rounded-md bg-accent/50">
            <p class="text-body-sm text-muted-foreground">
              This provider uses OAuth authentication. After creating the connection, you'll be redirected to authorize access.
            </p>
          </div>
{:else if meta.authType === 'none'}
<div class="space-y-3">
<div class="grid grid-cols-3 gap-2 rounded-lg border border-border/50 bg-muted/20 p-1">
<button type="button" class="rounded-md px-3 py-2 text-sm transition-colors cursor-pointer {noAuthMode === 'direct' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => noAuthMode = 'direct'}>
Direct
</button>
<button type="button" class="rounded-md px-3 py-2 text-sm transition-colors cursor-pointer {noAuthMode === 'http' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => noAuthMode = 'http'}>
HTTP Proxy
</button>
<button type="button" class="rounded-md px-3 py-2 text-sm transition-colors cursor-pointer {noAuthMode === 'relay' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}" onclick={() => noAuthMode = 'relay'}>
Relay
</button>
</div>

{#if noAuthMode !== 'direct'}
<div class="flex flex-col gap-1.5">
<Label class="text-body-sm-strong">{noAuthMode === 'http' ? 'HTTP proxy pool' : 'Relay proxy pool'}</Label>
<Select.Root type="single" value={selectedProxyPoolId} onValueChange={(v: string) => selectedProxyPoolId = v}>
								<Select.Trigger class="h-10 w-full text-body-sm">
									{availableProxyPools.find(p => p.id === selectedProxyPoolId)?.name ?? 'Select a proxy pool…'}
								</Select.Trigger>
								<Select.Content>
									<Select.Item value="">Select a proxy pool…</Select.Item>
									{#each availableProxyPools as pool}
        <Select.Item value={pool.id}>{pool.name} · {pool.proxyUrl}</Select.Item>
									{:else}
										<Select.Item value="" disabled>No active {noAuthMode} proxy pools</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
{#if loadingPools}
<div class="flex items-center gap-3 py-2">
  <div class="size-4 animate-pulse rounded-full bg-muted"></div>
  <div class="h-3 w-32 animate-pulse rounded bg-muted"></div>
</div>
{/if}
<p class="text-caption text-muted-foreground">Direct = shared AxonRouter egress. Proxy/Relay = distinct egress identity for this connection.</p>
</div>
{:else}
<div class="p-4 rounded-md bg-accent/50">
<p class="text-body-sm text-muted-foreground">Uses AxonRouter direct egress. Only one direct connection is allowed per no-auth provider; add proxy/relay accounts for rotation.</p>
</div>
{/if}
</div>
        {/if}

        {#if resultMsg}
          <p class="text-body-sm {resultOk ? 'text-emerald-400' : 'text-destructive'}">{resultMsg}</p>
        {/if}

        <div class="flex gap-3 pt-2">
          <Button onclick={handleAddConnection} disabled={loading} class="text-button-md rounded-sm px-5">
            {loading ? 'Adding...' : 'Add connection'}
          </Button>
          <Button onclick={() => step = 'select'} variant="ghost" class="text-body-sm">Back</Button>
        </div>
      </CardContent>
    </Card>

  {:else if step === 'done'}
    <Card class="shadow-card max-w-xl">
      <CardContent class="flex flex-col items-center justify-center py-16 text-center">
        <div class="size-12 rounded-full bg-emerald-500/10 flex items-center justify-center mb-4">
          <svg class="size-6 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <h3 class="text-body-md-strong mb-1">Connection added.</h3>
        <p class="text-body-sm text-muted-foreground mb-6">{resultMsg}</p>
        <div class="flex gap-3">
          <Button onclick={() => { step = 'select'; selectedProvider = null; resultMsg = ''; }} variant="outline" class="text-body-sm rounded-sm px-5">
            Add another
          </Button>
          <a href="/providers/{selectedProvider}" class="inline-flex items-center justify-center h-10 px-5 text-button-md bg-primary text-primary-foreground rounded-sm hover:opacity-90 transition-opacity">
            View provider
          </a>
        </div>
      </CardContent>
    </Card>
  {/if}
</div>
