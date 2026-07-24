<script lang="ts">
import { onMount } from 'svelte';
import { loadCombos, combos, isLoading, error, combosPagination } from '$lib/stores';
import { combosApi } from '$lib/api';
import { unwrapStr } from '$lib/utils';
import { Card, CardContent } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Switch } from '$lib/components/ui/switch';
import * as AlertDialog from '$lib/components/ui/alert-dialog';
import StatusBadge from '$lib/components/StatusBadge.svelte';
import ComboModal from '$lib/components/ComboModal.svelte';
import { toast } from 'svelte-sonner';
import PencilIcon from '@lucide/svelte/icons/pencil';
import Trash2Icon from '@lucide/svelte/icons/trash-2';
import type { Combo, ComboMetric } from '$lib/api';

let showCreate = $state(false);
let showEdit = $state(false);
let editingCombo = $state<Combo | null>(null);
let showDelete = $state(false);
let deleteTarget = $state<Combo | null>(null);
let deleteLoading = $state(false);
let metrics = $state<ComboMetric[]>([]);
let metricsTotals = $state<ComboMetric | null>(null);
let metricsLoading = $state(false);
let metricsError = $state<string | null>(null);

onMount(() => {
	document.title = 'Combos — AxonRouter';
	loadCombos();
	loadMetrics();
});

async function loadMetrics() {
	metricsLoading = true;
	metricsError = null;
	try {
		const res = await combosApi.metrics();
		metrics = res.data || [];
		metricsTotals = res.totals || null;
	} catch (err) {
		metricsError = err instanceof Error ? err.message : 'Failed to load combo metrics';
	} finally {
		metricsLoading = false;
	}
}

async function toggleCombo(combo: Combo) {
	try {
		await combosApi.update(combo.id, { is_active: !combo.is_active });
		toast.success(combo.is_active ? 'Combo disabled' : 'Combo enabled');
		await loadCombos();
		await loadMetrics();
	} catch (err) {
		toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown'));
	}
}

function confirmDelete(combo: Combo) {
  deleteTarget = combo;
  showDelete = true;
}

async function handleDelete() {
	if (!deleteTarget) return;
	deleteLoading = true;
	try {
		await combosApi.delete(deleteTarget.id);
		toast.success('Combo deleted');
		showDelete = false;
		await loadCombos();
		await loadMetrics();
	} catch (err) {
		toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		deleteLoading = false;
	}
}

const enabledCount = $derived($combos.filter(c => c.is_active).length);
const smartCount = $derived($combos.filter(c => c.is_smart).length);
const totalCombos = $derived($combosPagination.total || $combos.length);
const metricsMap = $derived(new Map(metrics.map((m) => [m.combo_id, m])));
const strategyOptions = ['priority', 'round-robin', 'weighted', 'random', 'least-used', 'fusion'];

function strategyLabel(opt: string) {
	if (opt === 'priority') return 'Priority';
	if (opt === 'round-robin') return 'Round Robin';
	if (opt === 'random') return 'Random';
	if (opt === 'least-used') return 'Least Used';
	if (opt === 'fusion') return 'Fusion';
	return 'Weighted';
}

function strategyDescription(opt: string) {
	if (opt === 'priority') return 'Try steps in order. First success wins.';
	if (opt === 'round-robin') return 'Rotate to a different step each request.';
	if (opt === 'random') return 'Pick a random step each request.';
	if (opt === 'least-used') return 'Prefer the model with the fewest recent successful calls.';
	if (opt === 'fusion') return 'Parallel panel + judge synthesis.';
	return 'Higher-weight steps are picked more often.';
}

function goToPage(page: number) {
  loadCombos(page, $combosPagination.per_page);
}

function openEdit(combo: Combo) {
  editingCombo = combo;
  showEdit = true;
}

async function handleSave() {
	await loadCombos();
	await loadMetrics();
}

$effect(() => {
  if (!showEdit) editingCombo = null;
});

$effect(() => {
  if (!showDelete) deleteTarget = null;
});
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="h-48 bg-muted animate-pulse rounded-md"></div>
    </div>
  {:else if $error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => loadCombos()} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else}
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Combos.</h1>
        <p class="text-body-sm text-muted-foreground">Build routing combos with ordered model steps, strategies, and smart goals.</p>
        <div class="flex items-center gap-3 text-body-sm text-muted-foreground">
          <span>{totalCombos} combos</span>
          <span class="text-border">·</span>
          <span class="inline-flex items-center gap-1">
            <span class="size-1.5 rounded-full bg-emerald-400"></span>
            {enabledCount} active
          </span>
          {#if smartCount > 0}
            <span class="text-border">·</span>
            <span class="inline-flex items-center gap-1">
              <span class="size-1.5 rounded-full bg-violet-400"></span>
              {smartCount} smart
            </span>
          {/if}
        </div>
      </div>
      <Button onclick={() => showCreate = true} class="text-button-md rounded-sm px-5">
        Add combo
      </Button>
	</div>

	<!-- Metrics cards -->
	{#if metricsLoading}
		<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
			{#each [1, 2, 3, 4] as _}
				<Card class="shadow-card p-4">
					<div class="h-12 bg-muted animate-pulse rounded-md"></div>
				</Card>
			{/each}
		</div>
	{:else if metricsError}
		<p class="text-caption text-destructive">Metrics: {metricsError}</p>
	{:else if metricsTotals}
		<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
			<Card class="shadow-card p-4">
				<p class="text-caption text-muted-foreground mb-1">Total requests</p>
				<p class="text-display-md">{metricsTotals.requests.toLocaleString()}</p>
			</Card>
			<Card class="shadow-card p-4">
				<p class="text-caption text-muted-foreground mb-1">Successes</p>
				<p class="text-display-md text-emerald-400">{metricsTotals.successes.toLocaleString()}</p>
			</Card>
			<Card class="shadow-card p-4">
				<p class="text-caption text-muted-foreground mb-1">Errors</p>
				<p class="text-display-md text-destructive">{metricsTotals.errors.toLocaleString()}</p>
			</Card>
			<Card class="shadow-card p-4">
				<p class="text-caption text-muted-foreground mb-1">Avg latency</p>
				<p class="text-display-md">{Math.round(metricsTotals.avg_latency_ms)} ms</p>
			</Card>
		</div>
	{/if}

	<!-- Table -->
    {#if $combos.length > 0}
      <Card class="shadow-card overflow-hidden p-0 flex flex-col">
        <div class="overflow-auto max-h-[60vh]">
          <table class="w-full text-body-sm">
            <thead>
              <tr class="border-b border-border bg-muted/50">
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Name</th>
                <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Strategy</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Timeout</th>
                <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Smart</th>
							<th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">State</th>
							<th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Errors (24h)</th>
							<th class="text-right text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody>
              {#each $combos as combo}
                <tr class="border-b border-border hover:bg-muted/50 transition-colors">
                  <td class="px-4 py-2.5">
                    <button
                      onclick={() => openEdit(combo)}
                      class="text-body-sm-strong hover:underline truncate block text-left cursor-pointer"
                    >
                      {combo.name}
                    </button>
                  </td>
                  <td class="px-4 py-2.5">
                    <span class="inline-flex items-center gap-1 text-caption-mono text-muted-foreground">
                      {#if combo.strategy === 'priority'}
                        <svg class="size-3 text-muted-foreground/60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12"/></svg>
                      {:else}
                        <svg class="size-3 text-muted-foreground/60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                      {/if}
                      {combo.strategy}
                    </span>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    <span class="text-caption-mono text-muted-foreground">{combo.timeout_ms >= 1000 ? (combo.timeout_ms / 1000) + 's' : combo.timeout_ms + 'ms'}</span>
                  </td>
                  <td class="px-4 py-2.5 text-center">
                    {#if combo.is_smart}
                      <StatusBadge status="smart" label={unwrapStr(combo.smart_goal) || 'on'} />
                    {:else}
                      <span class="text-caption-mono text-muted-foreground">—</span>
                    {/if}
                  </td>
							<td class="px-4 py-2.5 text-center">
								<div class="flex justify-center">
									<Switch checked={combo.is_active} onCheckedChange={() => toggleCombo(combo)} aria-label={combo.is_active ? 'Disable combo' : 'Enable combo'} />
								</div>
							</td>
							<td class="px-4 py-2.5 text-center">
								{#if metricsLoading}
									<span class="text-caption-mono text-muted-foreground">—</span>
								{:else}
									{@const m = metricsMap.get(combo.id)}
									<span class="text-caption-mono font-semibold {m && m.errors > 0 ? 'text-destructive' : 'text-muted-foreground'}">
										{m ? m.errors : 0}
									</span>
								{/if}
							</td>
							<td class="px-4 py-2.5 text-right">
                    <div class="flex gap-1 justify-end">
                      <Button
                        variant="outline"
                        size="icon"
                        class="size-7 rounded-sm"
                        onclick={() => openEdit(combo)}
                        title="Edit combo"
                        aria-label="Edit combo"
                      >
                        <PencilIcon class="size-3.5" />
                      </Button>
                      <Button
                        variant="destructive"
                        size="icon"
                        class="size-7 rounded-sm"
                        onclick={() => confirmDelete(combo)}
                        title="Delete combo"
                        aria-label="Delete combo"
                      >
                        <Trash2Icon class="size-3.5" />
                      </Button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </Card>
    {:else}
      <Card class="shadow-card">
        <CardContent class="flex flex-col items-center justify-center py-16">
          <div class="size-12 bg-muted rounded-md flex items-center justify-center mb-4">
            <svg class="size-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
            </svg>
          </div>
          <h3 class="text-body-md-strong mb-1">No combos configured.</h3>
          <p class="text-body-sm text-muted-foreground mb-4">
            Create a routing combo for load balancing, random selection, or smart routing.
          </p>
          <Button onclick={() => showCreate = true} class="text-button-md rounded-sm px-5">Add combo</Button>
        </CardContent>
      </Card>
    {/if}

	{#if $combosPagination.total_pages > 1}
		<div class="flex items-center justify-between">
			<span class="text-caption text-muted-foreground">
				Page {$combosPagination.page} of {$combosPagination.total_pages}
			</span>
			<div class="flex gap-2">
				<Button
					variant="outline"
					size="sm"
					class="text-caption-mono rounded-sm"
					disabled={$combosPagination.page <= 1}
					onclick={() => goToPage($combosPagination.page - 1)}
				>Prev</Button>
				<Button
					variant="outline"
					size="sm"
					class="text-caption-mono rounded-sm"
					disabled={$combosPagination.page >= $combosPagination.total_pages}
					onclick={() => goToPage($combosPagination.page + 1)}
				>Next</Button>
			</div>
		</div>
	{/if}

	<Card class="shadow-card p-4">
		<div class="space-y-4">
			<div>
				<p class="text-body-sm-strong text-foreground">Combo strategies</p>
				<p class="text-caption text-muted-foreground">
					Each combo uses one routing strategy. A request tries steps in the order shown until one succeeds, except for Fusion which runs all panels in parallel.
				</p>
			</div>
			<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
				{#each strategyOptions as opt}
					<div class="space-y-1 rounded-md border border-border bg-muted/30 p-3">
						<p class="text-body-sm-strong text-foreground">{strategyLabel(opt)}</p>
						<p class="text-caption text-muted-foreground">{strategyDescription(opt)}</p>
					</div>
				{/each}
			</div>
			<div class="space-y-1">
				<p class="text-body-sm-strong text-foreground">Smart combo</p>
				<p class="text-caption text-muted-foreground">
					Enable Smart combo to let the gateway pick this combo automatically when the user sends a goal keyword such as
					<span class="font-mono">auto</span>,
					<span class="font-mono">balanced</span>,
					<span class="font-mono">economy</span>, or
					<span class="font-mono">premium</span>.
					Selection uses live telemetry from request logs. If a combo name is requested directly, it always wins over smart routing.
				</p>
			</div>
		</div>
	</Card>
{/if}
</div>

<ComboModal bind:open={showCreate} combo={null} onSave={handleSave} />
<ComboModal bind:open={showEdit} combo={editingCombo} onSave={handleSave} />

<AlertDialog.Root bind:open={showDelete}>
  <AlertDialog.Content class="sm:max-w-md">
    <AlertDialog.Header>
      <AlertDialog.Title class="text-body-md-strong">Delete combo?</AlertDialog.Title>
      <AlertDialog.Description class="text-body-sm text-muted-foreground">
        This will permanently delete <span class="font-medium text-foreground">{deleteTarget?.name}</span> and all its routing steps. This action cannot be undone.
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel onclick={() => showDelete = false} class="rounded-sm">Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        onclick={handleDelete}
        disabled={deleteLoading}
        class="bg-destructive text-destructive-foreground hover:bg-destructive/90 rounded-sm"
      >
        {deleteLoading ? 'Deleting...' : 'Delete'}
      </AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
