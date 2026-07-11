<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog';
  import { Input } from '$lib/components/ui/input';
  import { ScrollArea } from '$lib/components/ui/scroll-area';
  import { Search } from '@lucide/svelte';
  import type { GatewayModel } from '$lib/api';

  let {
    open = $bindable(false),
    models = [] as GatewayModel[],
    selectedModel = '',
    onSelect,
  }: {
    open: boolean;
    models: GatewayModel[];
    selectedModel: string;
    onSelect: (modelId: string) => void;
  } = $props();

  let modelSearch = $state('');

  let filteredModels = $derived(
    models.filter((m) =>
      modelSearch ? m.id.toLowerCase().includes(modelSearch.toLowerCase()) : true,
    ),
  );

  function pick(modelId: string) {
    onSelect?.(modelId);
    open = false;
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="sm:max-w-lg max-h-[80vh] overflow-hidden flex flex-col gap-0 p-0">
    <div class="border-b border-border p-4">
      <Dialog.Title class="text-body-md-strong">Select model</Dialog.Title>
      <Dialog.Description class="text-caption text-muted-foreground">
        Browse all active models available on this gateway.
      </Dialog.Description>
      <div class="mt-3 flex items-center gap-2">
        <Search class="size-4 text-muted-foreground" />
        <Input
          bind:value={modelSearch}
          placeholder="Search models…"
          class="text-body-sm"
        />
      </div>
    </div>
    <ScrollArea class="flex-1 max-h-[50vh]">
      <div class="flex flex-col">
        {#each filteredModels as model (model.id)}
          <button
            class="flex items-center justify-between border-b border-border/50 px-4 py-2.5 text-left text-body-sm font-mono hover:bg-card/50 transition-colors cursor-pointer {selectedModel === model.id ? 'bg-primary/5 text-primary' : ''}"
            onclick={() => pick(model.id)}
          >
            <span class="truncate">{model.id}</span>
            <span class="ml-2 shrink-0 text-caption text-muted-foreground">{model.owned_by}</span>
          </button>
        {/each}
        {#if filteredModels.length === 0}
          <div class="px-4 py-6 text-center text-body-sm text-muted-foreground">
            No models found.
          </div>
        {/if}
      </div>
    </ScrollArea>
  </Dialog.Content>
</Dialog.Root>
