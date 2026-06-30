<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadCombo, selectedCombo, isLoading, error } from '$lib/stores';
  import { combosApi } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  
  let comboId = $derived($page.params.id);
  let actionLoading = $state('');
  let editing = $state(false);

  // Edit form state
  let editName = $state('');
  let editStrategy = $state('');
  let editTimeout = $state(0);
  let editStickyLimit = $state(0);
  let editIsActive = $state(true);
  
  onMount(() => {
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
        name: editName,
        strategy: editStrategy,
        timeout_ms: editTimeout,
        sticky_limit: editStickyLimit,
        is_active: editIsActive,
      });
      editing = false;
      await loadCombo(comboId);
    } catch (err) {
      alert('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      actionLoading = '';
    }
  }

  async function handleToggle() {
    if (!$selectedCombo) return;
    actionLoading = 'toggle';
    try {
      await combosApi.update(comboId, { is_active: !$selectedCombo.is_active });
      await loadCombo(comboId);
    } catch (err) {
      alert('Toggle failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      actionLoading = '';
    }
  }

  async function handleDelete() {
    if (!confirm('Delete this combo? This cannot be undone.')) return;
    actionLoading = 'delete';
    try {
      await combosApi.delete(comboId);
      window.location.href = '/combos';
    } catch (err) {
      alert('Delete failed: ' + (err instanceof Error ? err.message : 'Unknown'));
      actionLoading = '';
    }
  }

  const strategyOptions = ['priority', 'round-robin'];
  const smartGoalDescriptions: Record<string, string> = {
    auto: 'Automatically selects the best combo based on error rates and cost analysis.',
    economy: 'Prefers the lowest-cost combo options for budget-conscious routing.',
    balanced: 'Balances cost, latency, and quality for everyday workloads.',
    premium: 'Prefers highest quality regardless of cost for critical tasks.',
  };
</script>

<svelte:head>
  <title>{$selectedCombo?.name || 'Combo'} - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Breadcrumb -->
  <div class="flex items-center gap-2 text-body-sm text-muted-foreground">
    <a href="/combos" class="hover:text-foreground transition-colors">Combos</a>
    <span>/</span>
    <span class="text-foreground">{$selectedCombo?.name ?? 'Combo'}</span>
  </div>
  
  {#if $isLoading && !$selectedCombo}
    <div class="flex flex-col gap-6">
      <div class="h-8 w-64 bg-muted animate-pulse rounded-md"></div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div class="h-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-48 bg-muted animate-pulse rounded-md"></div>
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => loadCombo(comboId)} variant="outline">Try again</Button>
      </CardContent>
    </Card>
  {:else if $selectedCombo}
    {#if editing}
      <!-- Edit Mode -->
      <div class="space-y-1">
        <h1 class="text-display-lg">Edit combo.</h1>
        <p class="text-body-sm text-muted-foreground">Modify combo configuration.</p>
      </div>

      <Card class="shadow-vercel-2 border">
        <CardContent class="pt-6 space-y-6">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div class="space-y-2">
              <Label class="text-body-sm font-medium">Name</Label>
              <Input bind:value={editName} class="h-10 text-body-sm" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm font-medium">Strategy</Label>
              <div class="flex gap-2">
                {#each strategyOptions as opt}
                  <button
                    class="px-4 py-2 rounded-md text-body-sm border transition-colors {editStrategy === opt ? 'bg-foreground text-background border-foreground' : 'border-border text-muted-foreground hover:text-foreground'}"
                    onclick={() => editStrategy = opt}
                  >
                    {opt}
                  </button>
                {/each}
              </div>
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm font-medium">Timeout (ms)</Label>
              <Input type="number" bind:value={editTimeout} class="h-10 text-body-sm font-mono" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm font-medium">Sticky limit</Label>
              <Input type="number" bind:value={editStickyLimit} class="h-10 text-body-sm font-mono" />
            </div>
          </div>
          <div class="flex items-center gap-3">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={editIsActive} class="rounded" />
              <span class="text-body-sm">Active</span>
            </label>
          </div>
        </CardContent>
      </Card>

      <div class="flex gap-3">
        <Button onclick={handleSave} disabled={!!actionLoading} class="text-body-sm">
          {actionLoading === 'save' ? 'Saving...' : 'Save changes'}
        </Button>
        <Button onclick={() => editing = false} variant="ghost" class="text-body-sm">Cancel</Button>
      </div>
    {:else}
      <!-- View Mode -->
      <div class="space-y-1">
        <div class="flex items-center gap-3">
          <h1 class="text-display-lg">{$selectedCombo.name}.</h1>
          <Badge variant={$selectedCombo.is_active ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
            {$selectedCombo.is_active ? 'Active' : 'Inactive'}
          </Badge>
          {#if $selectedCombo.is_smart}
            <Badge variant="outline" class="text-caption-mono rounded-sm">Smart</Badge>
          {/if}
        </div>
        <p class="text-caption-mono text-muted-foreground">ID: {$selectedCombo.id}</p>
      </div>
      
      <!-- Details Grid -->
      <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card class="shadow-vercel-2 border">
          <CardHeader class="pb-3"><CardTitle class="text-body-md font-semibold">Configuration</CardTitle></CardHeader>
          <CardContent class="space-y-4">
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Strategy</p>
              <p class="text-body-sm font-medium">{$selectedCombo.strategy}</p>
            </div>
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Timeout</p>
              <p class="text-body-sm font-mono">{$selectedCombo.timeout_ms}ms</p>
            </div>
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Sticky limit</p>
              <p class="text-body-sm font-mono">{$selectedCombo.sticky_limit}</p>
            </div>
            <div class="space-y-1">
              <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Created</p>
              <p class="text-body-sm font-mono text-muted-foreground">{new Date($selectedCombo.created_at * 1000).toLocaleString()}</p>
            </div>
          </CardContent>
        </Card>
        
        <Card class="shadow-vercel-2 border">
          <CardHeader class="pb-3"><CardTitle class="text-body-md font-semibold">Smart settings</CardTitle></CardHeader>
          <CardContent>
            {#if $selectedCombo.is_smart && $selectedCombo.smart_goal}
              <div class="space-y-4">
                <div class="space-y-1">
                  <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Goal</p>
                  <p class="text-body-sm font-mono">{$selectedCombo.smart_goal}</p>
                </div>
                <div class="space-y-1">
                  <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Description</p>
                  <p class="text-body-sm text-muted-foreground">
                    {smartGoalDescriptions[$selectedCombo.smart_goal] ?? 'Custom smart routing goal.'}
                  </p>
                </div>
              </div>
            {:else}
              <p class="text-body-sm text-muted-foreground">Standard combo with fixed routing steps. Enable smart mode for goal-based resolution.</p>
            {/if}
          </CardContent>
        </Card>
      </div>
      
      <!-- Steps -->
      <Card class="shadow-vercel-2 border">
        <CardHeader class="flex flex-row items-center justify-between pb-3">
          <CardTitle class="text-body-md font-semibold">Routing steps</CardTitle>
        </CardHeader>
        <CardContent>
          <p class="text-body-sm text-muted-foreground">
            Define the order in which models are tried. Each step specifies a provider/model combination with the configured strategy.
          </p>
          <div class="mt-4 p-8 border border-dashed border-border rounded-md text-center text-body-sm text-muted-foreground">
            Steps editor coming soon. Configure via API for now.
          </div>
        </CardContent>
      </Card>
      
      <!-- Actions -->
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3"><CardTitle class="text-body-md font-semibold">Actions</CardTitle></CardHeader>
        <CardContent>
          <div class="flex flex-wrap gap-2">
            <Button onclick={startEdit} variant="outline" class="text-body-sm">Edit combo</Button>
            <Button onclick={handleToggle} disabled={!!actionLoading} variant="outline" class="text-body-sm">
              {$selectedCombo.is_active ? 'Deactivate' : 'Activate'}
            </Button>
            <Button onclick={handleDelete} disabled={!!actionLoading} variant="destructive" class="text-body-sm ml-auto">
              {actionLoading === 'delete' ? 'Deleting...' : 'Delete combo'}
            </Button>
          </div>
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>
