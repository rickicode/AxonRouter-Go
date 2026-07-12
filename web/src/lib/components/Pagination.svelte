<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import * as Select from '$lib/components/ui/select';

  let {
    page,
    totalPages,
    onChange,
    total = 0,
    perPage = 0,
    onPerPageChange,
    perPageOptions = [10, 20, 50],
  }: {
    page: number;
    totalPages: number;
    onChange: (page: number) => void;
    total?: number;
    perPage?: number;
    onPerPageChange?: (perPage: number) => void;
    perPageOptions?: number[];
  } = $props();

  // Visible page numbers with "..." gaps when there are many pages.
  const window = $derived.by(() => {
    if (totalPages <= 1) return [] as (number | '...')[];
    const cur = page;
    const last = totalPages;
    const set = new Set<number>([1, last, cur]);
    if (cur - 1 >= 1) set.add(cur - 1);
    if (cur + 1 <= last) set.add(cur + 1);
    const out: (number | '...')[] = [];
    let prev = 0;
    for (const p of [...set].sort((a, b) => a - b)) {
      if (prev && p - prev > 1) out.push('...');
      out.push(p);
      prev = p;
    }
    return out;
  });

  function go(p: number) {
    if (p < 1 || p > totalPages || p === page) return;
    onChange(p);
  }

  function handlePerPage(v: string) {
    if (!onPerPageChange) return;
    const n = Number(v);
    if (!Number.isNaN(n)) onPerPageChange(n);
  }
</script>

{#if totalPages > 1 || (perPage > 0 && onPerPageChange)}
  <div class="flex flex-wrap items-center justify-between gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <p class="text-caption text-muted-foreground">
        {#if total > 0}
          Page {page} of {totalPages} · {total} items
        {:else}
          Page {page} of {totalPages}
        {/if}
      </p>
      {#if perPage > 0 && onPerPageChange}
        <div class="flex items-center gap-1.5">
          <span class="text-caption text-muted-foreground">Per page</span>
          <Select.Root type="single" value={String(perPage)} onValueChange={handlePerPage}>
            <Select.Trigger class="h-8 w-[80px] text-body-sm rounded-sm">
              {perPage}
            </Select.Trigger>
            <Select.Content>
              {#each perPageOptions as opt}
                <Select.Item value={String(opt)} class="text-body-sm">{opt}</Select.Item>
              {/each}
            </Select.Content>
          </Select.Root>
        </div>
      {/if}
    </div>
    {#if totalPages > 1}
      <div class="flex items-center gap-1">
        <Button variant="outline" size="sm" disabled={page <= 1} onclick={() => go(page - 1)}>
          Prev
        </Button>
        {#each window as p}
          {#if p === '...'}
            <span class="px-1.5 text-caption text-muted-foreground select-none">…</span>
          {:else}
            <Button
              variant={p === page ? 'default' : 'outline'}
              size="sm"
              onclick={() => go(p)}
              aria-current={p === page ? 'page' : undefined}
            >
              {p}
            </Button>
          {/if}
        {/each}
        <Button variant="outline" size="sm" disabled={page >= totalPages} onclick={() => go(page + 1)}>
          Next
        </Button>
      </div>
    {/if}
  </div>
{/if}
