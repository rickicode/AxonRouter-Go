<script lang="ts">
  import { onMount } from 'svelte';
  import { loadProviders, providers, isLoading, error } from '$lib/stores';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import {
    CATEGORIES,
    getCategoryById,
    getProviderMeta,
    getStatusDotColor,
    getStatusVariant,
    getStatusLabel,
    type ProviderMeta,
  } from '$lib/provider-catalog';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';
  import type { Provider } from '$lib/api';
  import AddProviderModal from '$lib/components/AddProviderModal.svelte';
  let showAddModal = $state(false);

  let searchQuery = $state('');
  let activeCategory = $state('');
  let collapsed = $state<Record<string, boolean>>({});

  onMount(() => {
    document.title = 'Providers - AxonRouter';
    loadProviders();
  });

  function providerMeta(provider: Provider) {
    return getProviderMeta(provider.id);
  }

  function initials(value: string): string {
    const words = value.replace(/[^a-zA-Z0-9 ]/g, ' ').trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) return 'AI';
    if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
    return `${words[0][0]}${words[1][0]}`.toUpperCase();
  }

  function providerIconMeta(provider: Provider): ProviderMeta {
    const meta = providerMeta(provider);
    if (meta) return meta;
    const name = providerName(provider);
    return {
      id: provider.id,
      displayName: name,
      icon: 'api',
      textIcon: initials(name),
      category: 'compatible',
      description: 'Custom compatible provider endpoint.',
      format: provider.format || 'openai',
      authType: 'custom',
      prefix: `${provider.id}/`,
      isBuiltIn: false,
      color: '#f97316',
      serviceKinds: ['llm'],
    };
  }

  function providerCategoryId(provider: Provider): string {
    return providerMeta(provider)?.category ?? 'compatible';
  }

  function providerColor(provider: Provider): string {
    return providerMeta(provider)?.color ?? '#f97316';
  }

  function providerName(provider: Provider): string {
    return providerMeta(provider)?.displayName ?? provider.display_name ?? provider.id;
  }

  function providerDescription(provider: Provider): string {
    return providerMeta(provider)?.description ?? 'Custom compatible provider endpoint.';
  }

  function providerPrefix(provider: Provider): string {
    return providerMeta(provider)?.prefix ?? `${provider.id}/`;
  }

  function providerHasFree(provider: Provider): boolean {
    const meta = providerMeta(provider);
    return meta?.hasFree === true || meta?.category === 'no-auth';
  }


  function hexToRgba(color: string | undefined, alpha: number): string {
    const value = color?.trim();
    if (!value || !/^#[0-9a-fA-F]{6}$/.test(value)) return `rgb(23 23 23 / ${alpha})`;
    const r = parseInt(value.slice(1, 3), 16);
    const g = parseInt(value.slice(3, 5), 16);
    const b = parseInt(value.slice(5, 7), 16);
    return `rgb(${r} ${g} ${b} / ${alpha})`;
  }


  function issueCount(provider: Provider): number {
    return Object.entries(provider.status_counts ?? {}).reduce((sum, [status, count]) => {
      if (status === 'ready') return sum;
      return sum + Number(count || 0);
    }, 0);
  }

  function readyCount(provider: Provider): number {
    return Number(provider.status_counts?.ready ?? 0);
  }

  function categoryProviders(categoryId: string, source: Provider[]): Provider[] {
    if (categoryId === 'free') return source.filter(providerHasFree);
    return source.filter((provider) => providerCategoryId(provider) === categoryId);
  }


  function toggleCollapse(catId: string) {
    collapsed[catId] = !collapsed[catId];
  }

  let providerTotals = $derived.by(() => {
    const totalConnections = $providers.reduce((sum, provider) => sum + Number(provider.connection_count || 0), 0);
    const ready = $providers.reduce((sum, provider) => sum + readyCount(provider), 0);
    const issues = $providers.reduce((sum, provider) => sum + issueCount(provider), 0);
    const configured = $providers.filter((provider) => Number(provider.connection_count || 0) > 0).length;
    return { totalConnections, ready, issues, configured };
  });

  let categoryStats = $derived.by(() => {
    const stats: Record<string, { configured: number; total: number }> = {};
    for (const category of CATEGORIES) stats[category.id] = { configured: 0, total: 0 };
    for (const provider of $providers) {
      const categories = [providerCategoryId(provider)];
      if (providerHasFree(provider)) categories.push('free');
      for (const category of categories) {
        if (!stats[category]) stats[category] = { configured: 0, total: 0 };
        stats[category].total += 1;
        if (Number(provider.connection_count || 0) > 0) stats[category].configured += 1;
      }
    }
    return stats;
  });

  let visibleCategoryChips = $derived.by(() =>
    CATEGORIES.filter((category) => categoryStats[category.id]?.total > 0 || category.id === 'compatible'),
  );

  let filteredProviders = $derived.by(() => {
    let list = $providers;
    const q = searchQuery.trim().toLowerCase();
    if (q) {
      list = list.filter((provider) => {
        const meta = providerMeta(provider);
        const haystack = [
          provider.id,
          provider.display_name,
          providerName(provider),
          providerDescription(provider),
          provider.format,
          providerPrefix(provider),
          meta?.category,
          meta?.website,
          ...(meta?.serviceKinds ?? []),
        ]
          .filter(Boolean)
          .join(' ')
          .toLowerCase();
        return haystack.includes(q);
      });
    }
    if (activeCategory) list = categoryProviders(activeCategory, list);
    return list;
  });

  let groupedProviders = $derived.by(() => {
    const groups: Record<string, Provider[]> = {};
    for (const category of CATEGORIES) groups[category.id] = [];
    for (const provider of filteredProviders) {
      const categoryId = activeCategory === 'free' ? 'free' : providerCategoryId(provider);
      if (!groups[categoryId]) groups[categoryId] = [];
      groups[categoryId].push(provider);
    }
    return groups;
  });

  let visibleSections = $derived.by(() => {
    if (activeCategory === 'free') {
      const free = getCategoryById('free');
      return free && groupedProviders.free?.length ? [free] : [];
    }
    return CATEGORIES.filter((category) => category.id !== 'free' && groupedProviders[category.id]?.length);
  });
</script>

<div class="flex flex-1 flex-col gap-6 p-4 md:p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-6">
      <div class="rounded-xl bg-card p-5 shadow-card">
        <div class="h-8 w-48 animate-pulse rounded-md bg-muted"></div>
        <div class="mt-3 h-4 w-80 max-w-full animate-pulse rounded-md bg-muted/70"></div>
        <div class="mt-5 grid gap-3 md:grid-cols-4">
          {#each Array(4) as _}
            <div class="h-20 animate-pulse rounded-lg bg-muted/80"></div>
          {/each}
        </div>
      </div>
      <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {#each Array(8) as _}
          <div class="h-56 animate-pulse rounded-xl bg-card shadow-card"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <section class="rounded-xl border border-destructive/20 bg-destructive/5 p-8 text-center shadow-card">
      <p class="mx-auto max-w-xl text-body-sm text-destructive">{$error}</p>
      <Button onclick={loadProviders} variant="outline" class="mt-4 text-body-sm">Try again</Button>
    </section>
  {:else}
    <section class="overflow-hidden rounded-xl bg-card shadow-elevated">
      <div class="relative p-5 md:p-6">
        <div class="pointer-events-none absolute inset-x-0 top-0 h-32 bg-[radial-gradient(circle_at_20%_0%,rgba(0,124,240,0.16),transparent_34%),radial-gradient(circle_at_70%_0%,rgba(255,0,128,0.10),transparent_28%)]"></div>
        <div class="relative flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div class="max-w-3xl space-y-2">
            <h1 class="text-display-lg text-foreground">Providers</h1>
            <p class="text-body-sm text-muted-foreground">
              OmniRoute-style provider catalog with AxonRouter connection health, auth labels, prefixes, and model surface details.
            </p>
          </div>
          <button onclick={() => showAddModal = true} class="inline-flex items-center gap-2 h-9 rounded-lg bg-foreground px-4 text-sm font-medium text-background transition-all hover:bg-foreground/80 active:scale-[0.98]">
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
            Add provider
          </button>
        </div>

        <div class="relative mt-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-lg bg-background/80 p-4">
            <p class="text-caption text-muted-foreground">Catalog</p>
            <p class="mt-1 text-display-md">{$providers.length}</p>
            <p class="text-caption-mono text-muted-foreground">{providerTotals.configured} configured</p>
          </div>
          <div class="rounded-lg bg-background/80 p-4">
            <p class="text-caption text-muted-foreground">Connections</p>
            <p class="mt-1 text-display-md">{providerTotals.totalConnections}</p>
            <p class="text-caption-mono text-muted-foreground">runtime pool</p>
          </div>
          <div class="rounded-lg bg-background/80 p-4">
            <p class="text-caption text-muted-foreground">Ready</p>
            <p class="mt-1 text-display-md text-emerald-400">{providerTotals.ready}</p>
            <p class="text-caption-mono text-muted-foreground">available routes</p>
          </div>
          <div class="rounded-lg bg-background/80 p-4">
            <p class="text-caption text-muted-foreground">Needs attention</p>
            <p class="mt-1 text-display-md {providerTotals.issues > 0 ? 'text-destructive' : 'text-muted-foreground'}">{providerTotals.issues}</p>
            <p class="text-caption-mono text-muted-foreground">quota, auth, cooldown</p>
          </div>
        </div>
      </div>
    </section>

    <section class="rounded-xl bg-card p-4 shadow-card md:p-5">
      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div class="relative w-full lg:max-w-md">
            <Input
              type="text"
              class="h-10 w-full text-body-sm"
              placeholder="Search providers..."
              bind:value={searchQuery}
            />
            {#if searchQuery}
              <button
                type="button"
                class="absolute inset-y-0 right-2 text-caption text-muted-foreground hover:text-foreground"
                aria-label="Clear search"
                onclick={() => (searchQuery = '')}
              >
                Clear
              </button>
            {/if}
          </div>
          <div class="flex items-center gap-2 text-caption-mono text-muted-foreground">
            <span>{filteredProviders.length} shown</span>
            <span class="h-1 w-1 rounded-full bg-border"></span>
            <span>{visibleSections.length} sections</span>
          </div>
        </div>

        <div class="flex flex-wrap gap-2 border-t border-white/5 pt-4">
          <button
            type="button"
            class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-caption font-medium transition-colors {activeCategory === '' ? 'border-foreground bg-foreground text-background' : 'border-white/8 bg-background text-muted-foreground hover:border-foreground/30 hover:text-foreground'}"
            aria-pressed={activeCategory === ''}
            onclick={() => (activeCategory = '')}
          >
            All
            <span class="font-mono opacity-75">{providerTotals.configured}/{$providers.length}</span>
          </button>
          {#each visibleCategoryChips as cat (cat.id)}
            {@const stat = categoryStats[cat.id] ?? { configured: 0, total: 0 }}
            <button
              type="button"
              title={cat.description}
              class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-caption font-medium transition-colors {activeCategory === cat.id ? 'border-foreground bg-foreground text-background' : 'border-white/8 bg-background text-muted-foreground hover:border-foreground/30 hover:text-foreground'}"
              aria-pressed={activeCategory === cat.id}
              onclick={() => (activeCategory = activeCategory === cat.id ? '' : cat.id)}
            >
              <span class="h-2 w-2 rounded-full" style="background: {cat.color};"></span>
              <span>{cat.label}</span>
              <span class="font-mono opacity-75">{stat.configured}/{stat.total}</span>
            </button>
          {/each}
        </div>
      </div>
    </section>

    {#if filteredProviders.length > 0}
      <div class="flex flex-col gap-7">
        {#each visibleSections as cat (cat.id)}
          {@const sectionProviders = groupedProviders[cat.id] ?? []}
          {@const isCollapsed = collapsed[cat.id]}
          {@const stat = categoryStats[cat.id] ?? { configured: 0, total: 0 }}
          <section class="flex flex-col gap-3">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <button
                type="button"
                class="group flex min-w-0 items-start gap-3 text-left"
                onclick={() => toggleCollapse(cat.id)}
              >
                <span class="mt-2 h-2.5 w-2.5 shrink-0 rounded-full" style="background: {cat.color};"></span>
                <span class="min-w-0">
                  <span class="flex flex-wrap items-center gap-2">
                    <span class="text-display-sm text-foreground">{cat.id === 'free' ? 'Free tier providers' : `${cat.label} providers`}</span>
                    <Badge variant="secondary" class="rounded-full text-caption-mono">
                      {stat.configured}/{stat.total}
                    </Badge>
                  </span>
                  <span class="mt-1 block max-w-3xl text-body-sm text-muted-foreground">{cat.description}</span>
                </span>
                <span class="mt-1 text-body-md text-muted-foreground transition-transform" class:rotate-[-90deg]={isCollapsed}>⌄</span>
              </button>
            </div>

            {#if !isCollapsed}
              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
                {#each sectionProviders as provider (provider.id)}
                  {@const meta = providerMeta(provider)}
                  {@const iconMeta = providerIconMeta(provider)}
                  {@const color = providerColor(provider)}
                  {@const category = getCategoryById(providerCategoryId(provider))}
                  <a
                    href="/providers/{provider.id}"
                    class="group flex flex-col rounded-xl bg-card shadow-card transition-all duration-200 hover:-translate-y-0.5 hover:shadow-card-hover"
                  >
                    <div class="flex flex-col gap-2 p-3">
                      <div class="flex items-start gap-2.5">
                        <div
                          class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md"
                          style="background: {hexToRgba(color, 0.10)};"
                        >
                          <ProviderIcon meta={iconMeta} size={22} />
                        </div>
                        <div class="min-w-0 flex-1">
                          <div class="flex min-w-0 items-center gap-1.5">
                            <h3 class="min-w-0 flex-1 truncate text-[13px] font-semibold leading-tight text-foreground" title={providerName(provider)}>
                              {providerName(provider)}
                            </h3>
                            <span
                              class="inline-flex items-center gap-0.5 shrink-0"
                              title={category?.label ?? 'Compatible'}
                            >
                              <span class="h-2 w-2 rounded-full shrink-0" style="background: {category?.color ?? color};"></span>
                            </span>
                          </div>
                          <p class="truncate text-[11px] text-muted-foreground">{providerPrefix(provider)}</p>
                        </div>
                      </div>
                      <div class="flex items-center gap-1.5 text-[11px] text-muted-foreground pt-1.5">
                        {#if readyCount(provider) > 0}
                          <span class="inline-flex items-center gap-0.5 text-emerald-400">
                            <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
                            {readyCount(provider)} ready
                          </span>
                        {/if}
                        {#if issueCount(provider) > 0}
                          <span class="inline-flex items-center gap-0.5 text-amber-400">
                            <span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>
                            {issueCount(provider)} issues
                          </span>
                        {/if}
                        {#if readyCount(provider) === 0 && issueCount(provider) === 0}
                          <span class="text-muted-foreground">{provider.connection_count} conn</span>
                        {/if}
                        <span class="ml-auto text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity">
                          <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"></polyline></svg>
                        </span>
                      </div>
                    </div>
                  </a>
                {/each}
              </div>
            {/if}
          </section>
        {/each}
      </div>
    {:else}
      <section class="rounded-xl border border-dashed border-border/30 bg-card p-12 text-center shadow-card">
        <p class="text-body-sm text-muted-foreground">No providers match your filters.</p>
        <Button variant="outline" class="mt-4" onclick={() => { searchQuery = ''; activeCategory = ''; }}>
          Reset filters
        </Button>
      </section>
    {/if}
  {/if}
<AddProviderModal bind:open={showAddModal} onCreated={() => loadProviders()} />
</div>
