<script lang="ts">
  import { onMount } from 'svelte';
  import { router } from '$lib/router';
  import { loadCombo, selectedCombo, isLoading, error } from '$lib/stores';
  import { unwrapInt, unwrapStr } from '$lib/utils';
  import { combosApi } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Switch } from '$lib/components/ui/switch';
  import { toast } from 'svelte-sonner';

  let { id = '' }: { id?: string } = $props();
  let comboId = $derived(id);
  let actionLoading = $state('');
  let editing = $state(false);
  let editName = $state('');
  let editStrategy = $state('');
  let editTimeout = $state(0);
  let editStickyLimit = $state(0);
  let editIsActive = $state(true);

  onMount(() => {
    document.title = 'Combo — AxonRouter';
    loadCombo(comboId);
  });

  function startEdit() {
    if (!$selectedCombo) return;
    editName = $selectedCombo.name;
    editStrategy = $selectedCombo.strategy;
    editTimeout = $selectedCombo.timeout_ms;
    editStickyLimit = $selectedCombo.sticky_limit;
    editIsActive = $selectedCombo.is_active;
    editing = true;
  }

  async function handleSave() {
    actionLoading = 'save';
    try {
      await combosApi.update(comboId, {
        name: editName, strategy: editStrategy, timeout_ms: editTimeout,
        sticky_limit: editStickyLimit, is_active: editIsActive,
      });
      editing = false;
      toast.success('Combo updated');
      await loadCombo(comboId);
    } catch (err) { toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { actionLoading = ''; }
  }

  async function handleToggle() {
    if (!$selectedCombo) return;
    actionLoading = 'toggle';
    try {
      await combosApi.update(comboId, { is_active: !$selectedCombo.is_active });
      toast.success($selectedCombo.is_active ? 'Combo disabled' : 'Combo enabled');
      await loadCombo(comboId);
    }
    catch (err) { toast.error('Toggle failed: ' + (err instanceof Error ? err.message : 'Unknown')); }
    finally { actionLoading = ''; }
  }

  async function handleDelete() {
    actionLoading = 'delete';
    try { await combosApi.delete(comboId); toast.success('Combo deleted'); router.navigate('/combos'); }
    catch (err) { toast.error('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown')); actionLoading = ''; }
  }

  const strategyOptions = ['priority', 'round-robin'];
  const smartGoalDescriptions: Record<string, string> = {
    auto: 'Automatically selects the best combo based on error rates and cost analysis.',
    economy: 'Prefers the lowest-cost combo options for budget-conscious routing.',
    balanced: 'Balances cost, latency, and quality for everyday workloads.',
    premium: 'Prefers highest quality regardless of cost for critical tasks.',
  };
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Breadcrumb -->
  <div class="flex items-center gap-2 text-body-sm text-muted-foreground">
    <a href="/combos" class="hover:text-foreground transition-colors">Combos</a>
    <span>/</span>
    <span class="text-foreground">{$selectedCombo?.name ?? 'Combo'}</span>
  </div>

  {#if $isLoading && !$selectedCombo}
    <div class="h-48 bg-muted animate-pulse rounded-md"></div>
  {:else if $error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => loadCombo(comboId)} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else if $selectedCombo}
    {#if editing}
      <!-- Edit Mode -->
      <div class="space-y-1">
        <h1 class="text-display-lg">Edit combo.</h1>
      </div>
      <Card class="shadow-card">
        <CardContent class="pt-6 space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Name</Label>
              <Input bind:value={editName} class="h-10 text-body-sm" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Strategy</Label>
              <div class="flex gap-2">
                {#each strategyOptions as opt}
                  <button class="cursor-pointer px-4 py-2 rounded-sm text-body-sm border transition-colors {editStrategy === opt ? 'bg-foreground text-background border-foreground' : 'border-border text-muted-foreground hover:text-foreground'}" onclick={() => editStrategy = opt}>
                    {opt === 'priority' ? 'Priority' : 'Round Robin'}
                  </button>
                {/each}
              </div>
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Timeout (ms)</Label>
              <Input type="number" bind:value={editTimeout} class="h-10 text-code font-mono" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Sticky limit</Label>
              <Input type="number" bind:value={editStickyLimit} class="h-10 text-code font-mono" />
            </div>
          </div>
          <div class="flex items-center space-x-2">
            <Switch id="edit-is-active" bind:checked={editIsActive} />
            <Label for="edit-is-active" class="text-body-sm cursor-pointer">Active</Label>
          </div>
        </CardContent>
      </Card>
      <div class="flex gap-3">
        <Button onclick={handleSave} disabled={!!actionLoading} class="text-button-md rounded-sm px-5">
          {actionLoading === 'save' ? 'Saving...' : 'Save'}
        </Button>
        <Button onclick={() => editing = false} variant="ghost" class="text-body-sm">Cancel</Button>
      </div>
    {:else}
      <!-- View Mode -->
      <div class="flex items-center justify-between">
        <div class="space-y-1">
          <div class="flex items-center gap-3">
            <h1 class="text-display-lg">{$selectedCombo.name}.</h1>
            <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase
              {$selectedCombo.is_active
                ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                : 'bg-zinc-500/15 text-zinc-500 border border-zinc-500/20'}">
              <span class="size-1.5 rounded-full {$selectedCombo.is_active ? 'bg-emerald-400' : 'bg-zinc-600'}"></span>
              {$selectedCombo.is_active ? 'Active' : 'Inactive'}
            </span>
            {#if $selectedCombo.is_smart}
              <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase bg-violet-500/15 text-violet-400 border border-violet-500/30">
                Smart: {unwrapStr($selectedCombo.smart_goal) || 'on'}
              </span>
            {/if}
          </div>
          <p class="text-caption-mono text-muted-foreground">{$selectedCombo.id}</p>
        </div>
        <div class="flex gap-2">
          <Button onclick={startEdit} variant="outline" class="text-body-sm rounded-sm">Edit</Button>
          <Button onclick={handleToggle} disabled={!!actionLoading} variant="outline" class="text-body-sm rounded-sm">
            {$selectedCombo.is_active ? 'Disable' : 'Enable'}
          </Button>
          <Button onclick={handleDelete} disabled={!!actionLoading} variant="destructive" class="text-body-sm rounded-sm">
            {actionLoading === 'delete' ? 'Deleting...' : 'Delete'}
          </Button>
        </div>
      </div>

      <!-- Config summary as compact row -->
      <Card class="shadow-card">
        <CardContent class="py-4">
          <div class="grid grid-cols-4 gap-6">
            <div>
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Strategy</p>
              <p class="text-body-sm mt-0.5 flex items-center gap-1.5">
                {#if $selectedCombo.strategy === 'priority'}
                  <svg class="size-3.5 text-muted-foreground/60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12"/></svg>
                {:else}
                  <svg class="size-3.5 text-muted-foreground/60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                {/if}
                {$selectedCombo.strategy === 'priority' ? 'Priority' : 'Round Robin'}
              </p>
            </div>
            <div>
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Timeout</p>
              <p class="text-code font-mono mt-0.5">{$selectedCombo.timeout_ms >= 1000 ? ($selectedCombo.timeout_ms / 1000) + 's' : $selectedCombo.timeout_ms + 'ms'}</p>
            </div>
            <div>
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Sticky</p>
              <p class="text-code font-mono mt-0.5">{$selectedCombo.sticky_limit || '—'}</p>
            </div>
            <div>
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Created</p>
              <p class="text-caption text-muted-foreground mt-0.5">{unwrapInt($selectedCombo.created_at) ? new Date(unwrapInt($selectedCombo.created_at)! * 1000).toLocaleDateString() : '—'}</p>
            </div>
          </div>
          {#if $selectedCombo.is_smart && unwrapStr($selectedCombo.smart_goal)}
            <div class="mt-3 pt-3 border-t border-border">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Smart goal</p>
              <p class="text-body-sm mt-0.5">{smartGoalDescriptions[unwrapStr($selectedCombo.smart_goal) ?? ''] ?? 'Custom smart routing goal.'}</p>
            </div>
          {/if}
        </CardContent>
      </Card>

      <!-- Routing Steps -->
      <Card class="shadow-card">
        <CardHeader class="pb-3">
          <div class="flex items-center justify-between">
            <CardTitle class="text-body-md-strong">Routing steps</CardTitle>
            <span class="text-caption-mono text-muted-foreground">Configure via API</span>
          </div>
        </CardHeader>
        <CardContent>
          <div class="p-6 border border-dashed border-border rounded-md text-center">
            <p class="text-body-sm text-muted-foreground mb-1">No steps configured yet.</p>
            <p class="text-caption text-muted-foreground">Add connection/model steps via the API to define routing order.</p>
          </div>
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>
