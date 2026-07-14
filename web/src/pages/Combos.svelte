<script lang="ts">
import { onMount } from 'svelte';
import { loadCombos, combos, isLoading, error, combosPagination } from '$lib/stores';
  import { combosApi } from '$lib/api';
  import { unwrapStr } from '$lib/utils';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Switch } from '$lib/components/ui/switch';
  import * as Dialog from '$lib/components/ui/dialog';
import StatusBadge from '$lib/components/StatusBadge.svelte';
  import { toast } from 'svelte-sonner';

  let showCreate = $state(false);
  let createLoading = $state(false);
  let newName = $state('');
  let newStrategy = $state('priority');
  let newTimeout = $state(30000);
  let newStickyLimit = $state(0);
  let newIsSmart = $state(false);
  let newSmartGoal = $state('balanced');

  onMount(() => {
    document.title = 'Combos — AxonRouter';
    loadCombos();
  });

  async function handleCreate() {
    if (!newName.trim()) return;
    createLoading = true;
    try {
      await combosApi.create({
        name: newName.trim(),
        strategy: newStrategy,
        timeout_ms: newTimeout,
        sticky_limit: newStickyLimit,
        is_smart: newIsSmart,
        smart_goal: newIsSmart ? newSmartGoal : null,
        is_active: true,
      });
      toast.success('Combo created');
      showCreate = false;
      newName = '';
      newIsSmart = false;
      await loadCombos();
    } catch (err) {
      toast.error('Create failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      createLoading = false;
    }
  }

  async function toggleCombo(combo: any) {
    try {
      await combosApi.update(combo.id, { is_active: !combo.is_active });
      toast.success(combo.is_active ? 'Combo disabled' : 'Combo enabled');
      await loadCombos();
    } catch (err) {
      toast.error('Update failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function deleteCombo(id: string) {
    try {
      await combosApi.delete(id);
      toast.success('Combo deleted');
      await loadCombos();
    } catch (err) {
      toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

const strategyOptions = ['priority', 'round-robin', 'weighted'];
function strategyLabel(opt: string) {
  if (opt === 'priority') return 'Priority';
  if (opt === 'round-robin') return 'Round Robin';
  return 'Weighted';
}
const smartGoalOptions = [
  { value: 'auto', label: 'Auto', desc: 'Dynamic selection based on telemetry' },
  { value: 'economy', label: 'Economy', desc: 'Lowest cost routing' },
  { value: 'balanced', label: 'Balanced', desc: 'Cost, latency, quality balance' },
  { value: 'premium', label: 'Premium', desc: 'Highest quality regardless of cost' },
];

const enabledCount = $derived($combos.filter(c => c.is_active).length);
const smartCount = $derived($combos.filter(c => c.is_smart).length);
const totalCombos = $derived($combosPagination.total || $combos.length);

function goToPage(page: number) {
  loadCombos(page, $combosPagination.per_page);
}
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
        <Button onclick={loadCombos} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else}
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Combos.</h1>
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

    <!-- Table -->
    {#if $combos.length > 0}
      <Card class="shadow-card overflow-hidden p-0">
        <table class="w-full text-body-sm">
          <thead>
            <tr class="border-b border-border bg-muted/50">
              <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Name</th>
              <th class="text-left text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Strategy</th>
              <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Timeout</th>
              <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">Smart</th>
              <th class="text-center text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5">State</th>
              <th class="text-right text-caption-mono text-muted-foreground uppercase font-semibold px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody>
            {#each $combos as combo}
              <tr class="border-b border-border hover:bg-muted/50 transition-colors">
                <td class="px-4 py-2.5">
                  <a href="/combos/{combo.id}" class="text-body-sm-strong hover:underline truncate block ">{combo.name}</a>
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
                  <StatusBadge status="smart" label={unwrapStr(combo.smart_goal) || 'on'} />
                </td>
                <td class="px-4 py-2.5 text-center">
                  <StatusBadge status={combo.is_active ? 'active' : 'disabled'} label={combo.is_active ? 'On' : 'Off'} onclick={() => toggleCombo(combo)} />
                </td>
                <td class="px-4 py-2.5 text-right">
                  <div class="flex gap-1 justify-end">
                    <Button href="/combos/{combo.id}" variant="ghost" size="sm" class="text-caption-mono h-6 px-2 rounded-sm">Edit</Button>
                    <Button onclick={() => deleteCombo(combo.id)} variant="ghost" size="sm" class="text-caption-mono text-destructive h-6 px-2 rounded-sm">Del</Button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
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
            Create a routing combo for fallback, load balancing, or smart routing.
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
{/if}
</div>

<!-- Create Dialog -->
<Dialog.Root bind:open={showCreate}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title class="text-body-md-strong">Create combo</Dialog.Title>
    </Dialog.Header>
    <div class="space-y-4">
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Name</Label>
        <Input bind:value={newName} placeholder="e.g. fallback, premium-rr" class="h-10 text-body-sm" />
      </div>
      <div class="space-y-2">
        <Label class="text-body-sm-strong">Strategy</Label>
        <div class="flex gap-2">
          {#each strategyOptions as opt}
            <button
              class="cursor-pointer px-4 py-2 rounded-sm text-body-sm border transition-colors {newStrategy === opt ? 'bg-foreground text-background border-foreground' : 'border-border text-muted-foreground hover:text-foreground'}"
              onclick={() => newStrategy = opt}
> 
{strategyLabel(opt)}
</button>
          {/each}
        </div>
        <p class="text-caption text-muted-foreground">
          {newStrategy === 'priority'
            ? 'Try steps in order. First success wins.'
            : newStrategy === 'round-robin'
              ? 'Distribute requests across steps.'
              : 'Weighted-random order by step weight.'}
        </p>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div class="space-y-2">
          <Label class="text-body-sm-strong">Timeout</Label>
          <div class="flex items-center gap-1">
            <Input type="number" bind:value={newTimeout} class="h-10 text-code font-mono" />
            <span class="text-caption-mono text-muted-foreground whitespace-nowrap">ms</span>
          </div>
        </div>
        <div class="space-y-2">
          <Label class="text-body-sm-strong">Sticky limit</Label>
          <Input type="number" bind:value={newStickyLimit} min={0} class="h-10 text-code font-mono" />
        </div>
      </div>

<div class="flex items-center gap-3 pt-2 border-t border-border">
		<Switch id="new-is-smart" checked={newIsSmart} onCheckedChange={(v) => (newIsSmart = v)} />
		<Label for="new-is-smart" class="text-body-sm-strong cursor-pointer">Smart combo</Label>
	</div>

      {#if newIsSmart}
        <div class="space-y-2">
          <Label class="text-body-sm-strong">Goal</Label>
          <div class="grid grid-cols-2 gap-2">
            {#each smartGoalOptions as opt}
              <button
                class="cursor-pointer flex flex-col items-start gap-0.5 p-2.5 rounded-md border text-left transition-colors {newSmartGoal === opt.value ? 'border-foreground bg-accent' : 'border-border hover:border-foreground/50'}"
                onclick={() => newSmartGoal = opt.value}
              >
                <span class="text-body-sm-strong">{opt.label}</span>
                <span class="text-caption text-muted-foreground">{opt.desc}</span>
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="ghost" onclick={() => showCreate = false}>Cancel</Button>
      <Button onclick={handleCreate} disabled={createLoading || !newName.trim()}>
        {createLoading ? 'Creating...' : 'Create'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
