<script lang="ts">
	import { onMount } from 'svelte';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { modelPricingApi, type ModelPricing } from '$lib/api';

	let items = $state<ModelPricing[]>([]);
	let loading = $state(true);
	let error = $state('');

	let dialogOpen = $state(false);
	let editing = $state<ModelPricing | null>(null);
	let saving = $state(false);

	let deleteTarget = $state<ModelPricing | null>(null);
	let deleting = $state(false);

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

	const fmt = (n: number) => (n ? `$${n.toFixed(6)}` : '$0');

	onMount(load);
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="flex items-center justify-between">
		<div class="space-y-1">
			<h2 class="text-title-md font-semibold text-foreground">Model Pricing</h2>
			<p class="text-sm text-muted-foreground">
				Single source of truth for per-model cost rates. Used by cost estimation and smart combo scoring.
			</p>
		</div>
		<Button onclick={openAdd} class="gap-1.5 text-sm">
			<svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg>
			Add Model
		</Button>
	</div>

	{#if loading}
		<div class="rounded-xl bg-card p-8 shadow-card text-center text-sm text-muted-foreground">Loading…</div>
	{:else if error && items.length === 0}
		<div class="rounded-xl bg-card p-8 shadow-card text-center text-sm text-destructive">{error}</div>
	{:else if items.length === 0}
		<div class="rounded-xl bg-card p-8 shadow-card text-center text-sm text-muted-foreground">No pricing entries yet. Add one to get started.</div>
	{:else}
  <div class="rounded-xl bg-card shadow-card overflow-auto max-h-[70vh]">
			<table class="w-full text-sm">
				<thead>
					<tr class="border-b border-border text-left text-caption-mono text-muted-foreground">
						<th class="px-4 py-3 font-medium">Model ID</th>
						<th class="px-4 py-3 font-medium">Display Name</th>
						<th class="px-4 py-3 font-medium text-right">Input /1K</th>
						<th class="px-4 py-3 font-medium text-right">Output /1K</th>
						<th class="px-4 py-3 font-medium text-right">Reason /1K</th>
						<th class="px-4 py-3 font-medium text-right">Cache Read /1K</th>
						<th class="px-4 py-3 font-medium text-right">Cache Write /1K</th>
						<th class="px-4 py-3 font-medium">Cur</th>
						<th class="px-4 py-3 font-medium text-right">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each items as m (m.model_id)}
						<tr class="border-b border-border/50 hover:bg-muted/30">
							<td class="px-4 py-3 font-mono text-xs text-foreground">{m.model_id}</td>
							<td class="px-4 py-3 text-foreground">{m.display_name || '—'}</td>
							<td class="px-4 py-3 text-right font-mono text-xs">{fmt(m.input_per_1k)}</td>
							<td class="px-4 py-3 text-right font-mono text-xs">{fmt(m.output_per_1k)}</td>
							<td class="px-4 py-3 text-right font-mono text-xs">{fmt(m.reason_per_1k)}</td>
							<td class="px-4 py-3 text-right font-mono text-xs">{fmt(m.cached_read_per_1k)}</td>
							<td class="px-4 py-3 text-right font-mono text-xs">{fmt(m.cached_write_per_1k)}</td>
							<td class="px-4 py-3"><Badge variant="outline" class="rounded-full text-caption-mono">{m.currency || 'USD'}</Badge></td>
							<td class="px-4 py-3 text-right">
								<div class="flex justify-end gap-1.5">
									<Button variant="outline" size="sm" class="text-xs" onclick={() => openEdit(m)}>Edit</Button>
									<Button variant="destructive" size="sm" class="text-xs" onclick={() => askDelete(m)}>Delete</Button>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<!-- Add / Edit dialog -->
<Dialog.Root bind:open={dialogOpen}>
	<Dialog.Content class="sm:max-w-[640px]">
		<Dialog.Header>
			<Dialog.Title class="text-lg font-semibold">{editing ? 'Edit Model Pricing' : 'Add Model Pricing'}</Dialog.Title>
			<Dialog.Description class="text-sm text-muted-foreground">
				Rates are USD per 1,000 tokens unless noted. Cached/reason/image/audio are reference values; cost estimation currently bills input + output.
			</Dialog.Description>
		</Dialog.Header>

		<div class="grid grid-cols-2 gap-4 py-2">
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Model ID</Label>
				<Input bind:value={form.model_id} placeholder="gpt-4o" class="h-9 text-sm" disabled={!!editing} />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Display Name</Label>
				<Input bind:value={form.display_name} placeholder="GPT-4o" class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Input $/1K</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.input_per_1k} class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Output $/1K</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.output_per_1k} class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Reason $/1K</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.reason_per_1k} class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Cache Read $/1K</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.cached_read_per_1k} class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Cache Write $/1K</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.cached_write_per_1k} class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Currency</Label>
				<Input bind:value={form.currency} placeholder="USD" class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Image $/unit</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.image_per_unit} class="h-9 text-sm" />
			</div>
			<div class="flex flex-col gap-1.5">
				<Label class="text-sm font-medium">Audio $/min</Label>
				<Input type="number" step="0.000001" min="0" bind:value={form.audio_per_min} class="h-9 text-sm" />
			</div>
		</div>

		<Dialog.Footer>
			<Button variant="outline" onclick={() => (dialogOpen = false)} class="text-sm">Cancel</Button>
			<Button onclick={save} disabled={saving} class="text-sm">{saving ? 'Saving…' : 'Save'}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Delete confirm dialog -->
<Dialog.Root open={deleteTarget !== null} onOpenChange={(o) => { if (!o) deleteTarget = null; }}>
	<Dialog.Content class="sm:max-w-[420px]">
		<Dialog.Header>
			<Dialog.Title class="text-lg font-semibold">Delete pricing?</Dialog.Title>
			<Dialog.Description class="text-sm text-muted-foreground">
				Remove pricing for <span class="font-mono text-foreground">{deleteTarget?.model_id}</span>. This cannot be undone.
			</Dialog.Description>
		</Dialog.Header>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (deleteTarget = null)} class="text-sm">Cancel</Button>
			<Button variant="destructive" onclick={confirmDelete} disabled={deleting} class="text-sm">{deleting ? 'Deleting…' : 'Delete'}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
