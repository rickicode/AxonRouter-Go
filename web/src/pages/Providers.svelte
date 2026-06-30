<script lang="ts">
  import { onMount } from 'svelte';
  import { loadProviders, providers, isLoading, error } from '$lib/stores';
  import { Card, CardContent } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import {
    CATEGORIES,
    getProviderMeta,
    getStatusDotColor,
    getStatusVariant,
    getStatusLabel,
  } from '$lib/provider-catalog';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';
  import { providersApi } from '$lib/api';

  let searchQuery = $state('');
  let activeCategory = $state('');
  let testingId = $state<string | null>(null);
  let testResults = $state<Record<string, { ok: boolean; msg: string }>>({});
  let collapsed = $state<Record<string, boolean>>({});

  const filterCategories = CATEGORIES.filter((c) =>
    ['oauth', 'apikey', 'free', 'free_tier', 'compatible'].includes(c.id),
  );

  onMount(() => {
    document.title = 'Providers — AxonRouter';
    loadProviders();
  });

  let filteredProviders = $derived.by(() => {
    let list = $providers;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      list = list.filter(
        (p) =>
          p.display_name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q),
      );
    }
    if (activeCategory) {
      list = list.filter((p) => {
        const meta = getProviderMeta(p.id);
        const cat = meta?.category ?? 'compatible';
        return cat === activeCategory;
      });
    }
    return list;
  });

  let groupedProviders = $derived.by(() => {
    const groups: Record<string, typeof filteredProviders> = {};
    for (const cat of filterCategories) groups[cat.id] = [];
    for (const p of filteredProviders) {
      const meta = getProviderMeta(p.id);
      const cat = meta?.category ?? 'compatible';
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(p);
    }
    return groups;
  });

  async function handleTest(id: string) {
    testingId = id;
    try {
      const res = await providersApi.test(id);
      testResults[id] = { ok: res.success, msg: res.message ?? (res.success ? 'OK' : 'Failed') };
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Error';
      testResults[id] = { ok: false, msg };
    } finally {
      testingId = null;
    }
  }

  function toggleCollapse(catId: string) {
    collapsed[catId] = !collapsed[catId];
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  {#if $isLoading}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-48 animate-pulse rounded-md bg-muted"></div>
        <div class="h-4 w-72 animate-pulse rounded-md bg-muted/60"></div>
      </div>
      <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {#each Array(6) as _}
          <div class="h-48 animate-pulse rounded-md bg-muted"></div>
        {/each}
      </div>
    </div>
  {:else if $error}
    <Card class="border shadow-vercel-2">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="mb-4 text-body-sm text-muted-foreground">{$error}</p>
        <Button onclick={loadProviders} variant="outline">Try again</Button>
      </CardContent>
    </Card>
  {:else}
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div class="space-y-1">
        <h1 class="text-display-lg">Providers.</h1>
        <p class="text-body-sm text-muted-foreground">
          {$providers.length} providers configured.
        </p>
      </div>
      <a href="/providers/add" class="inline-flex items-center justify-center h-9 px-4 text-body-sm bg-foreground text-background rounded-md hover:bg-foreground/90 transition-colors">Add provider</a>
    </div>

    <!-- Filter bar -->
    <div class="flex flex-col gap-3">
      <Input
        type="text"
        class="h-9 max-w-sm text-body-sm"
        placeholder="Search providers..."
        bind:value={searchQuery}
      />
      <div class="flex flex-wrap gap-2">
        <button
          class="rounded-full border px-3 py-1 text-caption-mono transition-colors
            {!activeCategory
              ? 'border-foreground bg-foreground text-background'
              : 'border-border text-muted-foreground hover:border-foreground hover:text-foreground'}"
          onclick={() => (activeCategory = '')}
        >
          All
        </button>
        {#each filterCategories as cat}
          <button
            class="rounded-full border px-3 py-1 text-caption-mono transition-colors
              {activeCategory === cat.id
                ? 'border-foreground bg-foreground text-background'
                : 'border-border text-muted-foreground hover:border-foreground hover:text-foreground'}"
            onclick={() => (activeCategory = activeCategory === cat.id ? '' : cat.id)}
          >
            {cat.label}
          </button>
        {/each}
      </div>
    </div>

    <!-- Grouped provider sections -->
    {#if filteredProviders.length > 0}
      {#each filterCategories as cat}
        {#if groupedProviders[cat.id]?.length}
          {@const isCollapsed = collapsed[cat.id]}
          <button
            class="flex items-center gap-2 text-left"
            onclick={() => toggleCollapse(cat.id)}
          >
            <span class="text-body-md font-semibold text-foreground">{cat.label}</span>
            <Badge variant="secondary" class="rounded-full text-caption-mono">
              {groupedProviders[cat.id].length}
            </Badge>
            <span
              class="ml-1 text-xs text-muted-foreground transition-transform"
              class:rotate-[-90deg]={isCollapsed}
            >
              ▾
            </span>
          </button>

          {#if !isCollapsed}
            <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              {#each groupedProviders[cat.id] as provider (provider.id)}
                {@const meta = getProviderMeta(provider.id)}
                {@const color = meta?.color ?? '#888888'}
                {@const result = testResults[provider.id]}
                <a
                  href="/providers/{provider.id}"
                  class="group block rounded-lg border border-border bg-card shadow-vercel-2 transition-all hover:border-foreground/20 hover:bg-accent/10"
                >
                  <div class="flex flex-col gap-3 p-4">
                    <div class="flex items-center gap-3">
                      <div
                        class="flex shrink-0 items-center justify-center rounded-md overflow-hidden"
                        style="background: {color}15; width: 36px; height: 36px;"
                      >
                        <ProviderIcon {meta} size={36} />
                      </div>
                      <span class="truncate text-body-sm font-semibold text-foreground">
                        {provider.display_name}
                      </span>
                      <span
                        class="ml-auto h-2 w-2 shrink-0 rounded-full"
                        style="background: {cat.color};"
                      ></span>
                    </div>

                    {#if provider.status_counts}
                      <div class="flex flex-wrap gap-1.5">
                        {#each Object.entries(provider.status_counts) as [status, count]}
                          {#if count > 0}
                            <Badge
                              variant={getStatusVariant(status)}
                              class="gap-1 rounded-full py-0.5 text-caption-mono"
                            >
                              <span
                                class="inline-block h-1.5 w-1.5 rounded-full"
                                style="background: {getStatusDotColor(status)};"
                              ></span>
                              {getStatusLabel(status)}
                              {count}
                            </Badge>
                          {/if}
                        {/each}
                      </div>
                    {/if}

                    <div class="flex flex-wrap gap-1.5">
                      {#if provider.format}
                        <Badge variant="outline" class="rounded-full text-caption-mono">
                          {provider.format}
                        </Badge>
                      {/if}
                      {#if meta?.prefix}
                        <Badge variant="outline" class="rounded-full text-caption-mono">
                          {meta.prefix}
                        </Badge>
                      {/if}
                    </div>

                    <div class="flex gap-2 border-t border-border pt-3">
                      <span
                        class="flex-1 inline-flex items-center justify-center h-8 text-body-sm border border-border rounded-md"
                      >
                        Manage
                      </span>
                      <Button
                        variant="ghost"
                        size="sm"
                        class="text-body-sm"
                        disabled={testingId === provider.id}
                        onclick={(e: Event) => {
                          e.preventDefault();
                          e.stopPropagation();
                          handleTest(provider.id);
                        }}
                      >
                        {#if testingId === provider.id}
                          Testing…
                        {:else}
                          Test
                        {/if}
                      </Button>
                    </div>

                    {#if result}
                      <p
                        class="text-caption-mono {result.ok
                          ? 'text-emerald-500'
                          : 'text-destructive'}"
                      >
                        {result.msg}
                      </p>
                    {/if}
                  </div>
                </a>
              {/each}
            </div>
          {/if}
        {/if}
      {/each}
    {:else}
      <Card class="border shadow-vercel-2">
        <CardContent class="flex flex-col items-center justify-center py-16">
          <p class="text-body-sm text-muted-foreground">No providers match your filters.</p>
        </CardContent>
      </Card>
    {/if}
  {/if}
</div>
