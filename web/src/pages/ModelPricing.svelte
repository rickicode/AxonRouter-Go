<script lang="ts">
  import { onMount } from 'svelte';
  import * as Dialog from '$lib/components/ui/dialog';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Badge } from '$lib/components/ui/badge';
  import { toast } from 'svelte-sonner';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import SearchIcon from '@lucide/svelte/icons/search';
  import PencilIcon from '@lucide/svelte/icons/pencil';
  import TrashIcon from '@lucide/svelte/icons/trash-2';
  import DollarSignIcon from '@lucide/svelte/icons/dollar-sign';
  import { modelPricingApi, type ModelPricing } from '$lib/api';
  import Pagination from '$lib/components/Pagination.svelte';

  let items = $state<ModelPricing[]>([]);
  let loading = $state(true);
  let error = $state('');

  let dialogOpen = $state(false);
  let editing = $state<ModelPricing | null>(null);
  let saving = $state(false);

  let deleteTarget = $state<ModelPricing | null>(null);
  let deleting = $state(false);

  // Search + pagination + family filter
  let searchQuery = $state('');
  let selectedFamily = $state('All');
  let page = $state(1);
  let perPage = $state(24);

  const emptyForm = (): ModelPricing => ({
    model_id: '',
    display_name: '',
    input_per_1k: 0,
    output_per_1k: 0,
    reason_per_1k: 0,
    cached_read_per_1k: 0,
    cached_write_per_1k: 0,
    image_per_unit: 0,
    audio_per_min: 0,
    currency: 'USD',
    updated_at: 0,
  });

  let form = $state<ModelPricing>(emptyForm());

  // All available families for the filter
 let families = $derived.by(() => {
 const set = new Set<string>();
 for (const m of items) set.add(getModelFamily(m.model_id));
 return ['All', ...Array.from(set).sort()];
 });

 // Derived: filtered + paginated
 let filtered = $derived.by(() => {
 let out = items;
 if (selectedFamily !== 'All') {
 out = out.filter(m => getModelFamily(m.model_id) === selectedFamily);
 }
 if (searchQuery.trim()) {
 const q = searchQuery.toLowerCase();
 out = out.filter(m =>
 m.model_id.toLowerCase().includes(q) ||
 (m.display_name && m.display_name.toLowerCase().includes(q))
 );
 }
 return out;
 });

let totalPages = $derived(Math.max(1, Math.ceil(filtered.length / perPage)));
  let paged = $derived((() => {
    const start = (page - 1) * perPage;
    return filtered.slice(start, start + perPage);
  })());

  // Reset page on search
  $effect(() => {
    searchQuery;
    page = 1;
  });

  // Provider color from model_id prefix
  function getModelColor(id: string): string {
    const lower = id.toLowerCase();
    if (lower.includes('gpt') || lower.includes('o1') || lower.includes('o3') || lower.includes('o4')) return '#10a37f';
    if (lower.includes('claude')) return '#d97706';
    if (lower.includes('gemini')) return '#4285f4';
    if (lower.includes('deepseek')) return '#6366f1';
    if (lower.includes('llama')) return '#8b5cf6';
    if (lower.includes('mimo')) return '#ec4899';
    if (lower.includes('kimi')) return '#f59e0b';
    if (lower.includes('qwen')) return '#06b6d4';
    if (lower.includes('glm')) return '#14b8a6';
    if (lower.includes('minimax')) return '#f43f5e';
    if (lower.includes('hy3') || lower.includes('nemotron') || lower.includes('north')) return '#64748b';
    return '#71717a';
  }

  function getModelFamily(id: string): string {
    const lower = id.toLowerCase();
    if (lower.startsWith('gpt') || lower.startsWith('o1') || lower.startsWith('o3') || lower.startsWith('o4')) return 'OpenAI';
    if (lower.startsWith('claude')) return 'Anthropic';
    if (lower.startsWith('gemini')) return 'Google';
    if (lower.startsWith('deepseek')) return 'DeepSeek';
    if (lower.startsWith('llama')) return 'Meta';
    if (lower.startsWith('mimo')) return 'MiMo';
    if (lower.startsWith('kimi')) return 'Moonshot';
    if (lower.startsWith('qwen')) return 'Alibaba';
    if (lower.startsWith('glm')) return 'Zhipu';
    if (lower.startsWith('minimax')) return 'MiniMax';
    if (lower.startsWith('grok')) return 'xAI';
    return 'Other';
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await modelPricingApi.list();
      items = res.data ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load pricing';
      toast.error(error);
    } finally {
      loading = false;
    }
  }

  function openAdd() {
    editing = null;
    form = emptyForm();
    dialogOpen = true;
  }

  function openEdit(m: ModelPricing) {
    editing = m;
    form = { ...m };
    dialogOpen = true;
  }

  async function save() {
    if (!form.model_id.trim()) {
      toast.error('Model ID is required');
      return;
    }
    const rateFields = [
      'input_per_1k', 'output_per_1k', 'reason_per_1k',
      'cached_read_per_1k', 'cached_write_per_1k', 'image_per_unit', 'audio_per_min',
    ] as const;
    for (const f of rateFields) {
      const v = form[f];
      if (!Number.isFinite(v) || v < 0) {
        toast.error(`${f} must be a non-negative number`);
        return;
      }
    }
    const currency = form.currency.trim().toUpperCase();
    if (!/^[A-Z]{3}$/.test(currency)) {
      toast.error('Currency must be a 3-letter code (e.g. USD)');
      return;
    }
    form.currency = currency;
    saving = true;
    try {
      if (editing) {
        await modelPricingApi.update(form.model_id, form);
        toast.success('Pricing updated');
      } else {
        await modelPricingApi.create(form);
        toast.success('Pricing added');
      }
      dialogOpen = false;
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Save failed');
    } finally {
      saving = false;
    }
  }

  function askDelete(m: ModelPricing) {
    deleteTarget = m;
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    deleting = true;
    try {
      await modelPricingApi.delete(deleteTarget.model_id);
      toast.success('Pricing deleted');
      deleteTarget = null;
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Delete failed');
    } finally {
      deleting = false;
    }
  }

  const fmtShort = (n: number) => {
    if (!n) return '$0';
    if (n >= 1) return `$${n.toFixed(2)}`;
    if (n >= 0.01) return `$${n.toFixed(3)}`;
    if (n >= 0.001) return `$${n.toFixed(4)}`;
    return `$${n.toFixed(6)}`;
  };

  onMount(load);
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Header -->
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div class="space-y-1">
      <h1 class="text-display-lg">Model Pricing.</h1>
      <p class="text-body-sm text-muted-foreground">
        Canonical per-model cost rates. Providers reference these for usage tracking and cost estimation.
      </p>
  <p class="text-caption text-muted-foreground">
    Rates are per 1,000 tokens — multiply by 1,000 for the per-1M-token figure.
      </p>
    </div>
    <Button onclick={openAdd} class="gap-1.5 text-body-sm rounded-sm">
      <PlusIcon class="size-4" /> Add pricing
    </Button>
  </div>

  <!-- Search + family filter + stats -->
  <div class="flex flex-wrap items-center gap-3">
    <div class="relative flex-1 max-w-sm">
      <SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
      <Input bind:value={searchQuery} placeholder="Search models…" class="h-9 pl-9 text-body-sm" />
    </div>
    <select
      bind:value={selectedFamily}
      class="h-9 rounded-sm border border-border bg-card px-3 pr-8 text-body-sm text-foreground focus:outline-none focus:ring-1 focus:ring-ring cursor-pointer"
    >
      {#each families as f}
        <option value={f}>{f}</option>
      {/each}
    </select>
    <div class="flex items-center gap-3 text-body-sm text-muted-foreground">
      <span class="flex items-center gap-1.5">
        <DollarSignIcon class="size-3.5" />
        {filtered.length} model{filtered.length !== 1 ? 's' : ''}
      </span>
    </div>
  </div>

  {#if loading}
    <!-- Loading skeleton -->
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {#each Array(8) as _}
        <div class="h-44 rounded-xl bg-muted animate-pulse"></div>
      {/each}
    </div>
  {:else if error && items.length === 0}
    <div class="rounded-xl bg-card p-8 shadow-card text-center">
      <p class="text-body-sm text-destructive mb-4">{error}</p>
      <Button onclick={load} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
    </div>
  {:else if filtered.length === 0}
    <div class="rounded-xl bg-card p-8 shadow-card text-center">
      <p class="text-body-sm text-muted-foreground">
        {searchQuery ? 'No models match your search.' : 'No pricing entries yet. Add one to get started.'}
      </p>
    </div>
  {:else}
    <!-- Card grid -->
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
{#each paged as m (m.model_id)}
      {@const color = getModelColor(m.model_id)}
      {@const family = getModelFamily(m.model_id)}
      {@const isFree = !m.input_per_1k && !m.output_per_1k}
      {@const serviceKinds = m.service_kinds?.length === 1 && m.service_kinds[0] === 'llm' ? [] : (m.service_kinds ?? [])}
      <div class="group relative flex flex-col rounded-xl border border-border bg-card shadow-card hover:shadow-elevated transition-all duration-150">

        <div class="flex flex-1 flex-col p-4">
          <!-- Header: family badge + model name -->
          <div class="flex items-start justify-between gap-2 mb-3">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2 mb-1 flex-wrap">
                <span class="text-caption font-medium text-muted-foreground uppercase tracking-wider">{family}</span>
                {#if serviceKinds.length > 0}
                  {#each serviceKinds as kind (kind)}
                    <Badge variant="outline" class="text-[10px] px-1.5 py-0 rounded-full">{kind}</Badge>
                  {/each}
                {/if}
              </div>
              <p class="text-body-sm-strong truncate" title={m.display_name || m.model_id}>{m.display_name || m.model_id}</p>
              {#if m.display_name && m.display_name !== m.model_id}
                <p class="text-caption-mono text-muted-foreground truncate mt-0.5" title={m.model_id}>{m.model_id}</p>
              {/if}
            </div>
            {#if isFree}
              <Badge variant="outline" class="shrink-0 rounded-full border-emerald-500/30 text-emerald-400 text-caption-mono bg-emerald-500/10">Free</Badge>
            {:else}
              <Badge variant="outline" class="shrink-0 rounded-full text-caption-mono">{m.currency || 'USD'}</Badge>
            {/if}
          </div>

            <!-- Pricing -->
            {#if !isFree}
              <div class="flex-1">
                <!-- Primary rates -->
                <div class="grid grid-cols-2 gap-2 mb-2">
                  <div class="rounded-lg bg-muted/50 px-3 py-2">
                    <p class="text-caption text-muted-foreground mb-0.5">Input / 1K</p>
                    <p class="text-body-sm-strong font-mono">{fmtShort(m.input_per_1k)}</p>
                  </div>
                  <div class="rounded-lg bg-muted/50 px-3 py-2">
                    <p class="text-caption text-muted-foreground mb-0.5">Output / 1K</p>
                    <p class="text-body-sm-strong font-mono">{fmtShort(m.output_per_1k)}</p>
                  </div>
                </div>

                <!-- Secondary rates (only if non-zero) -->
                {#if m.reason_per_1k || m.cached_read_per_1k || m.cached_write_per_1k}
                  <div class="flex flex-wrap gap-x-4 gap-y-1 text-caption-mono text-muted-foreground">
                    {#if m.reason_per_1k}
                      <span>Reason: {fmtShort(m.reason_per_1k)} /1K</span>
                    {/if}
                    {#if m.cached_read_per_1k}
                      <span>Cache R: {fmtShort(m.cached_read_per_1k)} /1K</span>
                    {/if}
                    {#if m.cached_write_per_1k}
                      <span>Cache W: {fmtShort(m.cached_write_per_1k)} /1K</span>
                    {/if}
                  </div>
                {/if}
              </div>
            {:else}
              <div class="flex-1 flex items-center justify-center py-2">
                <p class="text-body-sm text-muted-foreground">Free tier — no cost</p>
              </div>
            {/if}

            <!-- Actions -->
            <div class="flex gap-1.5 border-t border-border pt-3 mt-3">
              <Button variant="ghost" size="sm" class="flex-1 gap-1 text-caption cursor-pointer" onclick={() => openEdit(m)}>
                <PencilIcon class="size-3" /> Edit
              </Button>
              <Button variant="ghost" size="sm" class="flex-1 gap-1 text-caption text-destructive cursor-pointer" onclick={() => askDelete(m)}>
                <TrashIcon class="size-3" /> Delete
              </Button>
            </div>
          </div>
        </div>
      {/each}
    </div>

    <!-- Pagination -->
    {#if totalPages > 1}
      <div class="mt-2">
        <Pagination {page} {totalPages} total={filtered.length} {perPage} onPerPageChange={(p) => { perPage = p; page = 1; }} onChange={(p) => page = p} />
      </div>
    {/if}
  {/if}
</div>

<!-- Add / Edit dialog -->
<Dialog.Root bind:open={dialogOpen}>
  <Dialog.Content class="sm:max-w-[640px]">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">{editing ? 'Edit Model Pricing' : 'Add Model Pricing'}</Dialog.Title>
      <Dialog.Description class="text-xs text-muted-foreground">
        Use canonical model names only (no provider prefix). Rates are per 1,000 tokens.
      </Dialog.Description>
    </Dialog.Header>

    <div class="grid grid-cols-2 gap-4 py-2">
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Model ID</Label>
        <Input bind:value={form.model_id} placeholder="gpt-4o" class="h-9 text-body-sm" disabled={!!editing} />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Display Name</Label>
        <Input bind:value={form.display_name} placeholder="GPT-4o" class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Input $/1K</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.input_per_1k} class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Output $/1K</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.output_per_1k} class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Reason $/1K</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.reason_per_1k} class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Cache Read $/1K</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.cached_read_per_1k} class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Cache Write $/1K</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.cached_write_per_1k} class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Currency</Label>
        <Input bind:value={form.currency} placeholder="USD" class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Image $/unit</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.image_per_unit} class="h-9 text-body-sm" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-sm font-medium">Audio $/min</Label>
        <Input type="number" step="0.000001" min="0" bind:value={form.audio_per_min} class="h-9 text-body-sm" />
      </div>
    </div>

    <Dialog.Footer>
      <Button variant="outline" onclick={() => (dialogOpen = false)} class="text-body-sm rounded-sm">Cancel</Button>
      <Button onclick={save} disabled={saving} class="text-body-sm rounded-sm">{saving ? 'Saving…' : 'Save'}</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Delete confirm dialog -->
<Dialog.Root open={deleteTarget !== null} onOpenChange={(o) => { if (!o) deleteTarget = null; }}>
  <Dialog.Content class="sm:max-w-[420px]">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">Delete pricing?</Dialog.Title>
      <Dialog.Description class="text-xs text-muted-foreground">
        Remove pricing for <span class="font-mono text-foreground">{deleteTarget?.model_id}</span>. This cannot be undone.
      </Dialog.Description>
    </Dialog.Header>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (deleteTarget = null)} class="text-body-sm rounded-sm">Cancel</Button>
      <Button variant="destructive" onclick={confirmDelete} disabled={deleting} class="text-body-sm rounded-sm">{deleting ? 'Deleting…' : 'Delete'}</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
