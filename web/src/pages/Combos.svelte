<script lang="ts">
import { onMount } from 'svelte';
import { loadCombos, combos, isLoading, error, combosPagination } from '$lib/stores';
import { combosApi } from '$lib/api';
import { unwrapStr } from '$lib/utils';
import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Badge } from '$lib/components/ui/badge';
import { Switch } from '$lib/components/ui/switch';
import StatusBadge from '$lib/components/StatusBadge.svelte';
import ComboModal from '$lib/components/ComboModal.svelte';
import { toast } from 'svelte-sonner';

let showCreate = $state(false);

onMount(() => {
	document.title = 'Combos — AxonRouter';
	loadCombos();
});

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
                <div class="flex justify-center">
                  <Switch checked={combo.is_active} onCheckedChange={() => toggleCombo(combo)} aria-label={combo.is_active ? 'Disable combo' : 'Enable combo'} />
                </div>
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

<ComboModal bind:open={showCreate} combo={null} onSave={() => loadCombos()} />
