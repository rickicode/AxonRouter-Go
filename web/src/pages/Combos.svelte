<script lang="ts">
  import { onMount } from 'svelte';
  import { loadCombos, combos, isLoading, error } from '$lib/stores';
  import { combosApi } from '$lib/api';
  import { unwrapStr } from '$lib/utils';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
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
      showCreate = false;
      newName = '';
      await loadCombos();
    } catch (err) {
      toast.error('Create failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      createLoading = false;
    }
  }

  const strategyOptions = ['priority', 'round-robin'];
  const smartGoalOptions = [
    { value: 'auto', label: 'Auto', desc: 'Dynamic selection based on telemetry' },
    { value: 'economy', label: 'Economy', desc: 'Lowest cost routing' },
    { value: 'balanced', label: 'Balanced', desc: 'Cost, latency, quality balance' },
    { value: 'premium', label: 'Premium', desc: 'Highest quality regardless of cost' },
  ];
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-72 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {#each Array(3) as _}
          <div class="h-56 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={loadCombos} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
      </CardContent>
    </Card>
  {:else}
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Combos.</h1>
        <p class="text-body-sm text-muted-foreground">
          {$combos.length} routing combos configured.
        </p>
      </div>
      <Button onclick={() => showCreate = !showCreate} class="text-button-md rounded-pill px-5">
        {showCreate ? 'Cancel' : 'Add combo'}
      </Button>
    </div>

    {#if showCreate}
      <Card class="shadow-card border border-primary/20">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md-strong">Create new combo.</CardTitle>
          <p class="text-body-sm text-muted-foreground">Configure a routing combo with ordered model steps.</p>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Name</Label>
              <Input bind:value={newName} placeholder="e.g. balanced, premium-failover" class="h-10 text-body-sm" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Strategy</Label>
              <div class="flex gap-2">
                {#each strategyOptions as opt}
                  <button
                    class="px-4 py-2 rounded-sm text-body-sm border transition-colors {newStrategy === opt ? 'bg-foreground text-background border-foreground' : 'border-white/8 text-muted-foreground hover:text-foreground'}"
                    onclick={() => newStrategy = opt}
                  >
                    {opt}
                  </button>
                {/each}
              </div>
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Timeout (ms)</Label>
              <Input type="number" bind:value={newTimeout} class="h-10 text-code font-mono" />
            </div>
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Sticky limit</Label>
              <Input type="number" bind:value={newStickyLimit} class="h-10 text-code font-mono" />
            </div>
          </div>

          <div class="flex items-center gap-3 pt-2 border-t border-white/5">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={newIsSmart} class="rounded" />
              <span class="text-body-sm-strong">Smart combo</span>
            </label>
            <span class="text-body-sm text-muted-foreground">Auto-resolve best combo based on goal</span>
          </div>

          {#if newIsSmart}
            <div class="space-y-2">
              <Label class="text-body-sm-strong">Smart goal</Label>
              <div class="grid grid-cols-2 md:grid-cols-4 gap-2">
                {#each smartGoalOptions as opt}
                  <button
                    class="flex flex-col items-start gap-1 p-3 rounded-md border text-left transition-colors {newSmartGoal === opt.value ? 'border-foreground bg-accent' : 'border-white/8 hover:border-foreground/50'}"
                    onclick={() => newSmartGoal = opt.value}
                  >
                    <span class="text-body-sm-strong">{opt.label}</span>
                    <span class="text-caption-mono text-muted-foreground">{opt.desc}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          <div class="flex gap-3 pt-2">
            <Button onclick={handleCreate} disabled={createLoading || !newName.trim()} class="text-button-md rounded-pill px-5">
              {createLoading ? 'Creating...' : 'Create combo'}
            </Button>
            <Button onclick={() => showCreate = false} variant="ghost" class="text-body-sm">Cancel</Button>
          </div>
        </CardContent>
      </Card>
    {/if}

    {#if $combos.length > 0}
      <div class="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {#each $combos as combo}
          <a href="/combos/{combo.id}" class="group block">
            <Card class="shadow-card transition-all hover:bg-accent/10 hover:border-foreground/20 h-full">
              <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-3">
                <div class="space-y-1">
                  <CardTitle class="text-body-md-strong">{combo.name}</CardTitle>
                  <p class="text-caption-mono text-muted-foreground">{combo.id}</p>
                </div>
                <div class="flex gap-1">
                  <Badge variant={combo.is_active ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
                    {combo.is_active ? 'Active' : 'Inactive'}
                  </Badge>
                  {#if combo.is_smart}
                    <Badge variant="outline" class="text-caption-mono rounded-sm">Smart</Badge>
                  {/if}
                </div>
              </CardHeader>
              <CardContent class="flex flex-col gap-4">
                <div class="grid grid-cols-2 gap-3 border-t border-white/5 pt-3">
                  <div>
                    <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Strategy</p>
                    <p class="text-body-sm mt-0.5">{combo.strategy}</p>
                  </div>
                  <div>
                    <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Timeout</p>
                    <p class="text-code font-mono mt-0.5">{combo.timeout_ms}ms</p>
                  </div>
                  {#if combo.is_smart && unwrapStr(combo.smart_goal)}
                    <div class="col-span-2">
                      <p class="text-caption-mono text-muted-foreground uppercase font-semibold">Smart goal</p>
                      <p class="text-code font-mono mt-0.5">{unwrapStr(combo.smart_goal)}</p>
                    </div>
                  {/if}
                </div>
              </CardContent>
            </Card>
          </a>
        {/each}
      </div>
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
            Create your first routing combo for fallback and load balancing.
          </p>
          <Button onclick={() => showCreate = true} class="text-button-md rounded-pill px-5">Add combo</Button>
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>
