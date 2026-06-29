<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadProvider, selectedProvider, loadConnections, connections, connectionPagination, connectionFilter, isLoading, error } from '$lib/stores';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import * as Select from '$lib/components/ui/select';
  
  let providerId = $derived($page.params.id);
  
  let currentPage = $state(1);
  let perPage = $state(50);
  
  onMount(() => {
    loadProvider(providerId);
    loadConnections(providerId, currentPage, perPage);
  });
  
  function handlePageChange(page: number) {
    currentPage = page;
    loadConnections(providerId, currentPage, perPage);
  }
  
  function handleFilterChange() {
    currentPage = 1;
    loadConnections(providerId, currentPage, perPage);
  }
  
  function getStatusVariant(status: string): 'default' | 'secondary' | 'destructive' {
    switch (status) {
      case 'ready':
        return 'default';
      case 'rate_limited':
      case 'quota_exhausted':
        return 'secondary';
      case 'balance_empty':
      case 'auth_failed':
      case 'suspended':
        return 'destructive';
      default:
        return 'secondary';
    }
  }

  const statusOptions = [
    { value: '', label: 'All statuses' },
    { value: 'ready', label: 'Ready' },
    { value: 'rate_limited', label: 'Rate limited' },
    { value: 'quota_exhausted', label: 'Quota exhausted' },
    { value: 'balance_empty', label: 'Balance empty' },
    { value: 'auth_failed', label: 'Auth failed' },
    { value: 'suspended', label: 'Suspended' },
    { value: 'disabled', label: 'Disabled' },
  ];
</script>

<svelte:head>
  <title>{$selectedProvider?.display_name || 'Provider'} - AxonRouter</title>
</svelte:head>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Back link -->
  <a href="/providers" class="inline-flex items-center gap-1.5 text-body-sm text-muted-foreground hover:text-foreground transition-colors w-fit">
    <svg class="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
    </svg>
    Back to providers
  </a>
  
  {#if $isLoading && !$selectedProvider}
    <div class="flex flex-col gap-6">
      <div class="space-y-2">
        <div class="h-8 w-64 bg-muted animate-pulse rounded-md"></div>
        <div class="h-4 w-48 bg-muted/60 animate-pulse rounded-md"></div>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div class="h-40 bg-muted animate-pulse rounded-md"></div>
        <div class="h-40 bg-muted animate-pulse rounded-md"></div>
      </div>
    </div>
  {:else if $error}
    <Card class="shadow-vercel-2 border">
      <CardContent class="flex flex-col items-center justify-center py-12">
        <p class="text-body-sm text-muted-foreground mb-4">{$error}</p>
        <Button onclick={() => { loadProvider(providerId); loadConnections(providerId, currentPage, perPage); }} variant="outline">
          Try again
        </Button>
      </CardContent>
    </Card>
  {:else if $selectedProvider}
    <!-- Page header -->
    <div class="space-y-1">
      <div class="flex items-center gap-3">
        <h1 class="text-display-lg">{$selectedProvider.display_name}.</h1>
        {#if $selectedProvider.is_custom}
          <Badge variant="secondary" class="text-caption-mono rounded-sm">Custom</Badge>
        {/if}
      </div>
      <div class="flex items-center gap-2 text-caption-mono text-muted-foreground">
        <span>Format: {$selectedProvider.format}</span>
        <span>·</span>
        <span>ID: {$selectedProvider.id}</span>
      </div>
    </div>
    
    <!-- Provider Info + Actions -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Details</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="space-y-1">
            <p class="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">Base URL</p>
            <p class="text-body-sm font-mono text-foreground break-all">{$selectedProvider.base_url}</p>
          </div>
        </CardContent>
      </Card>
      
      <Card class="shadow-vercel-2 border">
        <CardHeader class="pb-3">
          <CardTitle class="text-body-md font-semibold">Actions</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="flex flex-wrap gap-2">
            <Button href="/providers/{providerId}/test" variant="outline" size="sm" class="text-body-sm">
              Test all
            </Button>
            <Button href="/providers/{providerId}/bulk-add" variant="outline" size="sm" class="text-body-sm">
              Bulk add
            </Button>
            <Button href="/providers/{providerId}/add" size="sm" class="text-body-sm">
              Add connection
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
    
    <!-- Connections -->
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <h2 class="text-display-md">Connections.</h2>
        <span class="text-caption-mono text-muted-foreground">{$connectionPagination.total} total connections</span>
      </div>
      
      <!-- Filters -->
      <div class="flex flex-wrap gap-3">
        <Select.Root
          value={$connectionFilter.status}
          onValueChange={(value) => { $connectionFilter.status = value || ''; handleFilterChange(); }}
        >
          <Select.Trigger class="w-[180px] h-9 text-body-sm">
            {statusOptions.find(o => o.value === $connectionFilter.status)?.label || 'All statuses'}
          </Select.Trigger>
          <Select.Content>
            {#each statusOptions as option}
              <Select.Item value={option.value} class="text-body-sm">{option.label}</Select.Item>
            {/each}
          </Select.Content>
        </Select.Root>
        
        <Input
          type="text"
          class="w-64 h-9 text-body-sm"
          placeholder="Search connections..."
          bind:value={$connectionFilter.search}
          oninput={handleFilterChange}
        />
      </div>
      
      <!-- Connections Table -->
      <Card class="shadow-vercel-2 border overflow-hidden">
        <CardContent class="p-0">
          <div class="overflow-x-auto">
            <table class="w-full text-left border-collapse">
              <thead>
                <tr class="border-b border-border bg-muted/30">
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Name</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Status</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Auth type</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Failures</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Last success</th>
                  <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-28"></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#if $isLoading}
                  {#each Array(5) as _}
                    <tr>
                      {#each Array(6) as _}
                        <td class="p-4"><div class="h-4 bg-muted animate-pulse rounded-md"></div></td>
                      {/each}
                    </tr>
                  {/each}
                {:else if $connections.length === 0}
                  <tr>
                    <td colspan="6" class="p-8 text-center text-body-sm text-muted-foreground">
                      No active connections configured.
                    </td>
                  </tr>
                {:else}
                  {#each $connections as row}
                    <tr class="transition-colors hover:bg-accent/20">
                      <td class="py-3 px-4 font-medium text-body-sm">
                        <a href="/providers/{providerId}/{row.id}" class="hover:underline">
                          {row.name}
                        </a>
                      </td>
                      <td class="py-3 px-4">
                        <Badge variant={getStatusVariant(row.status)} class="text-caption-mono rounded-sm py-0.5">
                          {row.status}
                        </Badge>
                      </td>
                      <td class="py-3 px-4 font-mono text-xs text-muted-foreground">
                        {row.auth_type}
                      </td>
                      <td class="py-3 px-4 text-body-sm">
                        <span class={row.failure_count > 0 ? 'text-destructive font-medium' : 'text-muted-foreground'}>
                          {row.failure_count}
                        </span>
                      </td>
                      <td class="py-3 px-4 text-body-sm text-muted-foreground">
                        {row.last_success_at ? new Date(row.last_success_at * 1000).toLocaleString() : 'Never'}
                      </td>
                      <td class="py-3 px-4">
                        <div class="flex gap-1">
                          <Button href="/providers/{providerId}/{row.id}" variant="ghost" size="sm" class="text-body-sm h-8 px-2.5">
                            Edit
                          </Button>
                          <Button href="/providers/{providerId}/{row.id}/test" variant="ghost" size="sm" class="text-body-sm h-8 px-2.5">
                            Test
                          </Button>
                        </div>
                      </td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
      
      <!-- Pagination -->
      {#if $connectionPagination.total_pages > 1}
        <div class="flex items-center justify-between mt-4">
          <p class="text-body-sm text-muted-foreground">
            Showing {((currentPage - 1) * perPage) + 1}–{Math.min(currentPage * perPage, $connectionPagination.total)} of {$connectionPagination.total} connections
          </p>
          
          <div class="flex gap-1">
            <Button
              variant="outline"
              size="sm"
              disabled={currentPage === 1}
              onclick={() => handlePageChange(currentPage - 1)}
              class="text-body-sm h-8"
            >
              Prev
            </Button>
            
            {#each Array.from({ length: Math.min(5, $connectionPagination.total_pages) }, (_, i) => i + Math.max(1, currentPage - 2)) as page}
              {#if page <= $connectionPagination.total_pages}
                <Button
                  variant={page === currentPage ? 'default' : 'outline'}
                  size="sm"
                  onclick={() => handlePageChange(page)}
                  class="text-body-sm h-8 w-8 p-0"
                >
                  {page}
                </Button>
              {/if}
            {/each}
            
            <Button
              variant="outline"
              size="sm"
              disabled={currentPage === $connectionPagination.total_pages}
              onclick={() => handlePageChange(currentPage + 1)}
              class="text-body-sm h-8"
            >
              Next
            </Button>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>

