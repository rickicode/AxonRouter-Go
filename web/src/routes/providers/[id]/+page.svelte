<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { loadProvider, selectedProvider, loadConnections, connections, connectionPagination, connectionFilter, isLoading, error } from '$lib/stores';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import DataTable from '$lib/components/DataTable.svelte';
  
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
  
  function getStatusVariant(status: string) {
    switch (status) {
      case 'ready':
        return 'success';
      case 'rate_limited':
      case 'quota_exhausted':
        return 'warning';
      case 'balance_empty':
      case 'auth_failed':
      case 'suspended':
        return 'error';
      default:
        return 'neutral';
    }
  }
  
  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'status', label: 'Status' },
    { key: 'auth_type', label: 'Auth Type' },
    { key: 'failure_count', label: 'Failures' },
    { key: 'last_success_at', label: 'Last Success' },
    { key: 'actions', label: 'Actions' },
  ];
</script>

<svelte:head>
  <title>{$selectedProvider?.display_name || 'Provider'} - AxonRouter-Go</title>
</svelte:head>

<div class="min-h-screen bg-canvas">
  <!-- Header -->
  <section class="bg-canvas-dark text-on-dark py-3xl px-3xl">
    <div class="container-max">
      <div class="flex items-center gap-lg mb-lg">
        <Button href="/providers" variant="ghost" size="sm">
          <span class="mono-caps-button">← BACK</span>
        </Button>
      </div>
      
      {#if $selectedProvider}
        <span class="mono-caps text-on-dark/60 mb-lg block">PROVIDER</span>
        <h1 class="display-xl mb-lg">{$selectedProvider.display_name}</h1>
        <div class="flex flex-wrap gap-lg">
          <Badge variant="subtle">{$selectedProvider.format}</Badge>
          <Badge variant="subtle">{$selectedProvider.id}</Badge>
          {#if $selectedProvider.is_custom}
            <Badge variant="neutral">Custom</Badge>
          {/if}
        </div>
      {/if}
    </div>
  </section>
  
  <!-- Content -->
  <section class="section-padding">
    <div class="container-max">
      {#if $isLoading && !$selectedProvider}
        <div class="text-center py-3xl">
          <div class="animate-spin h-8 w-8 border-4 border-primary border-t-transparent rounded-full mx-auto mb-lg"></div>
          <p class="text-body text-body-md">Loading provider...</p>
        </div>
      {:else if $error}
        <Card variant="default" padding="lg">
          <div class="text-center">
            <p class="text-red-600 mb-lg">{$error}</p>
            <Button onclick={() => { loadProvider(providerId); loadConnections(providerId, currentPage, perPage); }} variant="outline">
              <span class="mono-caps-button">RETRY</span>
            </Button>
          </div>
        </Card>
      {:else if $selectedProvider}
        <!-- Provider Info -->
        <div class="grid grid-cols-1 tablet:grid-cols-2 gap-3xl mb-section">
          <Card>
            <h3 class="display-md mb-lg">Provider Details</h3>
            <div class="space-y-lg">
              <div>
                <span class="mono-caps text-body mb-xs block">ID</span>
                <span class="text-body-md">{$selectedProvider.id}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Display Name</span>
                <span class="text-body-md">{$selectedProvider.display_name}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Format</span>
                <span class="text-body-md">{$selectedProvider.format}</span>
              </div>
              <div>
                <span class="mono-caps text-body mb-xs block">Base URL</span>
                <span class="text-body-md text-body break-all">{$selectedProvider.base_url}</span>
              </div>
            </div>
          </Card>
          
          <Card>
            <h3 class="display-md mb-lg">Actions</h3>
            <div class="space-y-md">
              <Button href="/providers/{providerId}/test" variant="outline" class="w-full">
                <span class="mono-caps-button">TEST ALL CONNECTIONS</span>
              </Button>
              <Button href="/providers/{providerId}/bulk-add" variant="outline" class="w-full">
                <span class="mono-caps-button">BULK ADD CONNECTIONS</span>
              </Button>
              <Button href="/providers/{providerId}/add" variant="primary" class="w-full">
                <span class="mono-caps-button">ADD CONNECTION</span>
              </Button>
            </div>
          </Card>
        </div>
        
        <!-- Connections -->
        <div>
          <div class="flex items-center justify-between mb-3xl">
            <div>
              <h2 class="display-lg">Connections</h2>
              <p class="text-body-md text-body">{$connectionPagination.total} connections total</p>
            </div>
          </div>
          
          <!-- Filters -->
          <Card class="mb-3xl">
            <div class="flex flex-wrap gap-lg">
              <div class="flex-1 min-w-[200px]">
                <label for="conn-status" class="mono-caps text-body mb-xs block">STATUS</label>
                <select
                  id="conn-status"
                  class="input"
                  bind:value={$connectionFilter.status}
                  onchange={handleFilterChange}
                >
                  <option value="">All Statuses</option>
                  <option value="ready">Ready</option>
                  <option value="rate_limited">Rate Limited</option>
                  <option value="quota_exhausted">Quota Exhausted</option>
                  <option value="balance_empty">Balance Empty</option>
                  <option value="auth_failed">Auth Failed</option>
                  <option value="suspended">Suspended</option>
                  <option value="disabled">Disabled</option>
                </select>
              </div>
              
              <div class="flex-1 min-w-[200px]">
                <label for="conn-search" class="mono-caps text-body mb-xs block">SEARCH</label>
                <input
                  id="conn-search"
                  type="text"
                  class="input"
                  placeholder="Search connections..."
                  bind:value={$connectionFilter.search}
                  oninput={handleFilterChange}
                />
              </div>
            </div>
          </Card>
          
          <!-- Connections Table -->
          <DataTable
            {columns}
            data={$connections}
            loading={$isLoading}
            emptyMessage="No connections found"
          >
            {#snippet cell({ column, row })}
              {#if column.key === 'name'}
                <a href="/providers/{providerId}/{row.id}" class="text-ink hover:text-primary font-medium">
                  {row.name}
                </a>
              {:else if column.key === 'status'}
                <Badge variant={getStatusVariant(row.status)}>
                  {row.status}
                </Badge>
              {:else if column.key === 'auth_type'}
                <span class="mono-caps">{row.auth_type}</span>
              {:else if column.key === 'failure_count'}
                <span class={row.failure_count > 0 ? 'text-red-600' : 'text-body'}>
                  {row.failure_count}
                </span>
              {:else if column.key === 'last_success_at'}
                <span class="text-body">
                  {row.last_success_at ? new Date(row.last_success_at * 1000).toLocaleString() : 'Never'}
                </span>
              {:else if column.key === 'actions'}
                <div class="flex gap-xs">
                  <Button href="/providers/{providerId}/{row.id}" variant="ghost" size="sm">
                    <span class="mono-caps-button">VIEW</span>
                  </Button>
                  <Button href="/providers/{providerId}/{row.id}/test" variant="ghost" size="sm">
                    <span class="mono-caps-button">TEST</span>
                  </Button>
                </div>
              {:else}
                {row[column.key] || '-'}
              {/if}
            {/snippet}
          </DataTable>
          
          <!-- Pagination -->
          {#if $connectionPagination.total_pages > 1}
            <div class="flex items-center justify-between mt-3xl">
              <p class="text-body-md text-body">
                Showing {((currentPage - 1) * perPage) + 1} to {Math.min(currentPage * perPage, $connectionPagination.total)} of {$connectionPagination.total} connections
              </p>
              
              <div class="flex gap-xs">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={currentPage === 1}
                  onclick={() => handlePageChange(currentPage - 1)}
                >
                  <span class="mono-caps-button">PREV</span>
                </Button>
                
                {#each Array.from({ length: Math.min(5, $connectionPagination.total_pages) }, (_, i) => i + Math.max(1, currentPage - 2)) as page}
                  {#if page <= $connectionPagination.total_pages}
                    <Button
                      variant={page === currentPage ? 'primary' : 'outline'}
                      size="sm"
                      onclick={() => handlePageChange(page)}
                    >
                      <span class="mono-caps-button">{page}</span>
                    </Button>
                  {/if}
                {/each}
                
                <Button
                  variant="outline"
                  size="sm"
                  disabled={currentPage === $connectionPagination.total_pages}
                  onclick={() => handlePageChange(currentPage + 1)}
                >
                  <span class="mono-caps-button">NEXT</span>
                </Button>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </section>
</div>
