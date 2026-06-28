<script lang="ts">
  export let columns: { key: string; label: string; class?: string }[];
  export let data: any[];
  export let loading = false;
  export let emptyMessage = 'No data available';
</script>

<div class="overflow-x-auto">
  <table class="w-full border-collapse">
    <thead>
      <tr class="data-table-header">
        {#each columns as column}
          <th class="px-lg py-md text-left {column.class || ''}">
            {column.label}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#if loading}
        <tr>
          <td colspan={columns.length} class="px-lg py-md text-center text-body">
            <div class="flex items-center justify-center gap-sm">
              <svg class="animate-spin h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              Loading...
            </div>
          </td>
        </tr>
      {:else if data.length === 0}
        <tr>
          <td colspan={columns.length} class="px-lg py-md text-center text-body">
            {emptyMessage}
          </td>
        </tr>
      {:else}
        {#each data as row, index}
          <tr class="data-table-row hover:bg-hairline/50 transition-colors">
            {#each columns as column}
              <td class="data-table-cell {column.class || ''}">
                <slot name="cell" {column} {row} {index}>
                  {row[column.key] || '-'}
                </slot>
              </td>
            {/each}
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>
