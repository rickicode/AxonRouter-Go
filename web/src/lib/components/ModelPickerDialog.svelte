<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
import SearchIcon from '@lucide/svelte/icons/search';
import CheckIcon from '@lucide/svelte/icons/check';
import { Badge } from '$lib/components/ui/badge';
import type { GatewayModel } from '$lib/api';

	let {
		open = $bindable(false),
		models = [] as GatewayModel[],
		selectedModel = '',
		selectedModels = [] as string[],
		onSelect,
		onMultiSelect,
		multi = false,
	}: {
		open: boolean;
		models: GatewayModel[];
		selectedModel?: string;
		selectedModels?: string[];
		onSelect?: (modelId: string) => void;
		onMultiSelect?: (modelIds: string[]) => void;
		multi?: boolean;
	} = $props();

	let modelSearch = $state('');
	let localSelection = $state<Set<string>>(new Set());

	$effect(() => {
		if (open) {
			localSelection = new Set(multi ? selectedModels : selectedModel ? [selectedModel] : []);
		}
	});

	let filteredModels = $derived(
		models.filter((m) =>
			modelSearch ? m.id.toLowerCase().includes(modelSearch.toLowerCase()) : true,
		),
	);

	function toggle(modelId: string) {
		const next = new Set(localSelection);
		if (next.has(modelId)) {
			next.delete(modelId);
		} else {
			next.add(modelId);
		}
		localSelection = next;
		if (!multi) {
			onSelect?.(modelId);
			open = false;
		}
	}

	function confirm() {
		if (multi) {
			onMultiSelect?.(Array.from(localSelection));
		}
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-lg max-h-[85vh] overflow-hidden flex flex-col gap-0 p-0">
		<div class="border-b border-border p-4">
			<Dialog.Title class="text-body-md-strong">
				{multi ? 'Select models' : 'Select model'}
			</Dialog.Title>
			<Dialog.Description class="text-caption text-muted-foreground">
				Browse all active models available on this gateway.
			</Dialog.Description>
			<div class="mt-3 flex items-center gap-2">
				<SearchIcon class="size-4 text-muted-foreground" />
				<Input bind:value={modelSearch} placeholder="Search models…" class="text-body-sm" />
			</div>
		</div>
		<ScrollArea class="flex-1 min-h-0">
			<div class="flex flex-col">
{#each filteredModels as model (model.id)}
      {@const kinds = model.service_kinds?.length === 1 && model.service_kinds[0] === 'llm' ? [] : (model.service_kinds ?? [])}
      <button
        class="flex items-center justify-between border-b border-border/50 px-4 py-2.5 text-left text-body-sm font-mono hover:bg-card/50 transition-colors cursor-pointer {localSelection.has(model.id) ? 'bg-primary/5 text-primary' : ''}"
        onclick={() => toggle(model.id)}
      >
        <span class="truncate">{model.id}</span>
        <div class="flex items-center gap-2">
          {#if kinds.length > 0}
            {#each kinds as kind (kind)}
              <Badge variant="outline" class="shrink-0 text-[10px] px-1.5 py-0 rounded-full">{kind}</Badge>
            {/each}
          {/if}
          <span class="shrink-0 text-caption text-muted-foreground">{model.owned_by}</span>
          {#if localSelection.has(model.id)}
            <CheckIcon class="size-3.5 text-primary" />
          {/if}
        </div>
      </button>
    {/each}
				{#if filteredModels.length === 0}
					<div class="px-4 py-6 text-center text-body-sm text-muted-foreground">
						No models found.
					</div>
				{/if}
			</div>
		</ScrollArea>
		{#if multi}
			<div class="border-t border-border p-4">
				<Button class="w-full" onclick={confirm}>
					Done ({localSelection.size} selected)
				</Button>
			</div>
		{/if}
	</Dialog.Content>
</Dialog.Root>
