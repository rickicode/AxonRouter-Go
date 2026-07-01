<script lang="ts">
  import { onMount } from 'svelte';
  import { loadLogs, logs, logPagination, logFilter, isLoading, error, formatTimestamp, formatLatency, formatTokens, formatCost } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Terminal, Filter, RefreshCw, Download } from '@lucide/svelte';

  let currentPage = $state(1);
  let perPage = $state(100);

  onMount(() => {
    document.title = 'Logs — AxonRouter';
    loadLogs(currentPage, perPage);
  });

  function handlePageChange(page: number) {
    currentPage = page;
    loadLogs(currentPage, perPage);
  }

  function handleFilterChange() {
    currentPage = 1;
    loadLogs(currentPage, perPage);
  }

  function setStatusFilter(code: number) {
    $logFilter.status_code = code;
    handleFilterChange();
  }

  function clearFilters() {
    $logFilter = { provider_id: '', connection_id: '', model_id: '', status_code: 0, start_date: '', end_date: '' };
    handleFilterChange();
  }

  function handleExport() {
    const headers = ['Time', 'Provider', 'Model', 'Modality', 'Status', 'Latency', 'Input', 'Output', 'Cached', 'Cost'];
    const rows = $logs.map(row => [
      formatTimestamp(row.timestamp),
      row.provider_type_id,
      row.model_id,
      row.modality,
      row.status_code.toString(),
      `${row.latency_ms}ms`,
      row.input_tokens.toString(),
      row.output_tokens.toString(),
      row.cached_tokens.toString(),
      `$${row.cost_usd.toFixed(4)}`,
    ]);
    const csv = [headers.join(','), ...rows.map(r => r.join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `axonrouter-logs-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function getStatusBadgeProps(statusCode: number) {
    if (statusCode >= 200 && statusCode < 300) return { label: `${statusCode} OK`, variant: 'default' as const };
    if (statusCode === 429) return { label: `${statusCode} RATE LIMITED`, variant: 'secondary' as const };
    if (statusCode >= 400 && statusCode < 500) return { label: `${statusCode} CLIENT ERR`, variant: 'secondary' as const };
    if (statusCode >= 500) return { label: `${statusCode} SERVER ERR`, variant: 'destructive' as const };
    return { label: `${statusCode}`, variant: 'secondary' as const };
  }

  let hasActiveFilters = $derived(
    $logFilter.provider_id || $logFilter.connection_id || $logFilter.model_id || $logFilter.status_code || $logFilter.start_date || $logFilter.end_date
  );

  const columns = [
    { key: 'timestamp', label: 'Time' },
    { key: 'provider_type_id', label: 'Provider' },
    { key: 'model_id', label: 'Model' },
    { key: 'modality', label: 'Modality' },
    { key: 'status_code', label: 'Status' },
    { key: 'latency_ms', label: 'Latency' },
    { key: 'input_tokens', label: 'Input' },
    { key: 'output_tokens', label: 'Output' },
    { key: 'cached_tokens', label: 'Cached' },
    { key: 'cost_usd', label: 'Cost' },
  ];
</script>

<div class="flex flex-1 flex-col gap-6 p-6 w-full">
  <div class="flex items-center justify-between">
    <div class="space-y-1">
      <h1 class="text-display-lg">Logs.</h1>
      <p class="text-body-sm text-muted-foreground">
        Request tracing, latency tracking, and token usage analytics.
      </p>
    </div>
    <div class="flex items-center gap-2">
      <Button onclick={handleExport} disabled={$logs.length === 0} variant="outline" size="sm" class="text-body-sm h-9 rounded-sm">
        <Download class="size-3.5 mr-1.5" />
        Export CSV
      </Button>
      <Button onclick={() => loadLogs(currentPage, perPage)} disabled={$isLoading} variant="outline" size="sm" class="text-body-sm h-9 rounded-sm">
        <RefreshCw class="size-3.5 mr-1.5 {$isLoading ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>
  </div>

  <Card class="shadow-card">
    <CardHeader class="pb-3 border-b border-white/5 flex flex-row items-center justify-between space-y-0">
      <div class="flex items-center gap-2">
        <Filter class="size-4 text-muted-foreground" />
        <CardTitle class="text-body-md-strong">Filters</CardTitle>
        {#if hasActiveFilters}
          <Button onclick={clearFilters} variant="ghost" size="sm" class="text-caption-mono h-6 px-2 text-muted-foreground">
            Clear all
          </Button>
        {/if}
      </div>
      <!-- DESIGN.md tab-ghost pills for status filters -->
      <div class="flex items-center gap-1.5 overflow-x-auto">
        {#each [
          { code: 0, label: 'All' },
          { code: 200, label: '200 OK' },
          { code: 429, label: '429 Limited' },
          { code: 401, label: '401 Auth' },
          { code: 500, label: '5xx Error' },
        ] as pill}
          <button
            class="rounded-pill-sm px-3 py-1 text-caption-mono transition-colors
              {$logFilter.status_code === pill.code
                ? 'bg-foreground text-background'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
            onclick={() => setStatusFilter(pill.code)}
          >
            {pill.label}
          </button>
        {/each}
      </div>
    </CardHeader>

    <CardContent class="pt-4">
      <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <div class="space-y-1.5">
          <label for="filter-provider" class="text-caption-mono text-muted-foreground uppercase font-semibold">Provider</label>
          <Input id="filter-provider" placeholder="openai, cx, mimo..." class="h-9 font-mono text-body-sm" bind:value={$logFilter.provider_id} oninput={handleFilterChange} />
        </div>
        <div class="space-y-1.5">
          <label for="filter-model" class="text-caption-mono text-muted-foreground uppercase font-semibold">Model</label>
          <Input id="filter-model" placeholder="gpt-4o, claude-sonnet..." class="h-9 font-mono text-body-sm" bind:value={$logFilter.model_id} oninput={handleFilterChange} />
        </div>
        <div class="space-y-1.5">
          <label for="filter-start" class="text-caption-mono text-muted-foreground uppercase font-semibold">From date</label>
          <Input id="filter-start" type="date" class="h-9 font-mono text-body-sm" bind:value={$logFilter.start_date} oninput={handleFilterChange} />
        </div>
        <div class="space-y-1.5">
          <label for="filter-end" class="text-caption-mono text-muted-foreground uppercase font-semibold">To date</label>
          <Input id="filter-end" type="date" class="h-9 font-mono text-body-sm" bind:value={$logFilter.end_date} oninput={handleFilterChange} />
        </div>
      </div>
    </CardContent>
  </Card>

  {#if $isLoading}
    <div class="flex flex-col gap-3">
      {#each Array(8) as _}
        <div class="h-12 bg-card/60 animate-pulse rounded-md"></div>
      {/each}
    </div>
  {:else if $error}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16 text-center">
        <p class="text-destructive font-medium text-body-sm mb-4">{$error}</p>
        <Button onclick={() => loadLogs(currentPage, perPage)} variant="outline" size="sm" class="text-body-sm">Retry</Button>
      </CardContent>
    </Card>
  {:else if $logs.length === 0}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-16 text-center">
        <Terminal class="size-8 text-muted-foreground mb-3" />
        <p class="text-foreground font-semibold text-body-sm mb-1">No logs found.</p>
        <p class="text-muted-foreground text-body-sm">Adjust filters or send requests to the proxy to generate logs.</p>
      </CardContent>
    </Card>
  {:else}
    <Card class="shadow-card overflow-hidden">
      <CardContent class="p-0">
        <div class="overflow-x-auto">
          <table class="w-full text-left border-collapse">
            <thead>
              <tr class="border-b border-white/5 bg-muted/30">
                {#each columns as column}
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">{column.label}</th>
                {/each}
              </tr>
            </thead>
            <tbody class="divide-y divide-border/60">
              {#each $logs as row}
                {@const statusProps = getStatusBadgeProps(row.status_code)}
                <tr class="transition-colors hover:bg-accent/20">
                  <td class="py-3 px-4 font-mono text-caption text-muted-foreground whitespace-nowrap">{formatTimestamp(row.timestamp)}</td>
                  <td class="py-3 px-4 text-body-sm-strong">
                    <a href="/providers/{row.provider_type_id}" class="hover:underline text-foreground">{row.provider_type_id}</a>
                  </td>
                  <td class="py-3 px-4 text-code text-foreground">{row.model_id}</td>
                  <td class="py-3 px-4"><Badge variant="secondary" class="text-caption-mono rounded-sm py-0.5">{row.modality}</Badge></td>
                  <td class="py-3 px-4"><Badge variant={statusProps.variant} class="text-caption-mono rounded-sm py-0.5">{statusProps.label}</Badge></td>
                  <td class="py-3 px-4 text-code text-muted-foreground">{formatLatency(row.latency_ms)}</td>
                  <td class="py-3 px-4 text-code text-muted-foreground">{formatTokens(row.input_tokens)}</td>
                  <td class="py-3 px-4 text-code text-muted-foreground">{formatTokens(row.output_tokens)}</td>
                  <td class="py-3 px-4 text-code text-muted-foreground">{formatTokens(row.cached_tokens)}</td>
                  <td class="py-3 px-4 text-code text-foreground font-medium">{formatCost(row.cost_usd)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>

    {#if $logPagination.total_pages > 1}
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pt-2">
        <p class="text-body-sm text-muted-foreground">
          Showing <strong class="text-foreground">{((currentPage - 1) * perPage) + 1}–{Math.min(currentPage * perPage, $logPagination.total)}</strong> of <strong class="text-foreground">{$logPagination.total}</strong> logs
        </p>
        <div class="flex items-center gap-1">
          <Button variant="outline" size="sm" disabled={currentPage === 1} onclick={() => handlePageChange(currentPage - 1)} class="text-body-sm h-8 rounded-sm">Prev</Button>
          {#each Array.from({ length: Math.min(5, $logPagination.total_pages) }, (_, i) => i + Math.max(1, currentPage - 2)) as page}
            {#if page <= $logPagination.total_pages}
              <Button variant={page === currentPage ? 'default' : 'outline'} size="sm" onclick={() => handlePageChange(page)} class="text-body-sm h-8 w-8 p-0 rounded-sm">{page}</Button>
            {/if}
          {/each}
          <Button variant="outline" size="sm" disabled={currentPage === $logPagination.total_pages} onclick={() => handlePageChange(currentPage + 1)} class="text-body-sm h-8 rounded-sm">Next</Button>
        </div>
      </div>
    {/if}
  {/if}
</div>
