<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import SearchIcon from '@lucide/svelte/icons/search';
	import CheckIcon from '@lucide/svelte/icons/check';
	import Loader2Icon from '@lucide/svelte/icons/loader-2';
	import type { GatewayModel, Provider } from '$lib/api';
	import { providersApi } from '$lib/api';

	let {
		open = $bindable(false),
		models = [] as GatewayModel[],
		providers: providersProp = undefined as Provider[] | undefined,
		selectedModel = '',
		selectedModels = [] as string[],
		onSelect,
		onMultiSelect,
		multi = false,
	}: {
		open: boolean;
		models: GatewayModel[];
		providers?: Provider[];
		selectedModel?: string;
		selectedModels?: string[];
		onSelect?: (modelId: string) => void;
		onMultiSelect?: (modelIds: string[]) => void;
		multi?: boolean;
	} = $props();

	let modelSearch = $state('');
	let selectedProvider = $state<string>('all');
	let localSelection = $state<Set<string>>(new Set());
	let fetchedProviders = $state<Provider[]>([]);
	let loadingProviders = $state(false);

	let effectiveProviders = $derived(providersProp ?? fetchedProviders);
	let activeProviders = $derived(
		effectiveProviders.filter((p) => p.connection_count > 0),
	);
	let activeProviderMap = $derived(new Map(activeProviders.map((p) => [p.id, p])));

	let activeModels = $derived(
		models.filter((m) => {
			const prefix = m.id.split('/')[0] || m.id;
			return activeProviderMap.has(prefix);
		}),
	);

	let visibleModels = $derived(
		activeModels.filter((m) => {
			if (!modelSearch) return true;
			return m.id.toLowerCase().includes(modelSearch.toLowerCase());
		}).filter((m) => {
			if (selectedProvider === 'all') return true;
			return m.id === selectedProvider || m.id.startsWith(`${selectedProvider}/`);
		}),
	);

	let grouped = $derived(() => {
		const map = new Map<string, GatewayModel[]>();
		for (const m of visibleModels) {
			const prefix = m.id.split('/')[0] || m.id;
			const arr = map.get(prefix) ?? [];
			arr.push(m);
			map.set(prefix, arr);
		}
		return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
	});

	$effect(() => {
		if (open) {
			localSelection = new Set(multi ? selectedModels : selectedModel ? [selectedModel] : []);
			if (!providersProp) {
				loadingProviders = true;
				providersApi
					.list()
					.then((res) => {
						fetchedProviders = res.data ?? [];
					})
					.catch(() => {
						fetchedProviders = [];
					})
					.finally(() => {
						loadingProviders = false;
					});
			}
		}
	});

	function providerDisplay(id: string): string {
		return activeProviderMap.get(id)?.display_name ?? id;
	}

	function providerCount(id: string): number {
		return activeProviderMap.get(id)?.connection_count ?? 0;
	}

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
	<Dialog.Content class="flex max-h-[85vh] w-full flex-col overflow-hidden p-0 sm:max-w-5xl">
		<!-- Header -->
		<div class="border-b border-border p-5">
			<div class="flex items-start justify-between gap-4">
				<div>
					<Dialog.Title class="text-body-md-strong">
						{multi ? 'Select models' : 'Select model'}
					</Dialog.Title>
					<Dialog.Description class="text-caption text-muted-foreground">
						Only models from providers with active connections are shown.
					</Dialog.Description>
				</div>
				{#if multi}
					<Badge variant="secondary" class="shrink-0">
						{localSelection.size} selected
					</Badge>
				{/if}
			</div>

			<div class="mt-4 flex items-center gap-2">
				<SearchIcon class="size-4 text-muted-foreground" />
				<Input
					bind:value={modelSearch}
					placeholder="Search models…"
					class="h-10 flex-1 text-body-sm"
				/>
			</div>

			<!-- Provider filter chips -->
			<div class="mt-4 flex flex-wrap gap-2">
				<Button
					variant={selectedProvider === 'all' ? 'default' : 'outline'}
					size="sm"
					class="h-8 gap-1.5 rounded-full px-3 text-caption"
					onclick={() => (selectedProvider = 'all')}
				>
					All
					<Badge variant="secondary" class="h-4 px-1 text-[10px]">{activeModels.length}</Badge>
				</Button>
				{#each activeProviders as provider (provider.id)}
					{@const modelCount = activeModels.filter((m) => m.id === provider.id || m.id.startsWith(`${provider.id}/`)).length}
					<Button
						variant={selectedProvider === provider.id ? 'default' : 'outline'}
						size="sm"
						class="h-8 gap-1.5 rounded-full px-3 text-caption"
						onclick={() => (selectedProvider = provider.id)}
					>
						{provider.display_name}
						<Badge variant="secondary" class="h-4 px-1 text-[10px]">{modelCount}</Badge>
					</Button>
				{/each}
			</div>
		</div>

		<!-- Scrollable model list -->
		<div class="flex-1 overflow-y-auto p-4">
			{#if loadingProviders}
				<div class="flex flex-col items-center justify-center gap-2 py-12">
					<Loader2Icon class="size-5 animate-spin text-muted-foreground" />
					<p class="text-body-sm text-muted-foreground">Loading providers…</p>
				</div>
			{:else if activeProviders.length === 0}
				<div class="px-6 py-12 text-center">
					<p class="text-body-sm text-muted-foreground">
						No active providers found. Add a connection first.
					</p>
				</div>
			{:else if visibleModels.length === 0}
				<div class="px-6 py-12 text-center">
					<p class="text-body-sm text-muted-foreground">
						No models match the selected provider or search.
					</p>
				</div>
			{:else}
				<div class="flex flex-col gap-6">
					{#each grouped() as [prefix, groupModels] (prefix)}
						<div>
							<div class="mb-2 flex items-center gap-2">
								<h4 class="text-body-sm-strong">{providerDisplay(prefix)}</h4>
								<Badge variant="outline" class="h-4 px-1.5 text-[10px]">
									{providerCount(prefix)} connection{providerCount(prefix) === 1 ? '' : 's'}
								</Badge>
								<span class="text-caption text-muted-foreground">
									{groupModels.length} model{groupModels.length === 1 ? '' : 's'}
								</span>
							</div>
							<div class="grid grid-cols-1 gap-2 md:grid-cols-2 lg:grid-cols-3">
								{#each groupModels as model (model.id)}
									{@const selected = localSelection.has(model.id)}
									{@const kinds = model.service_kinds?.length === 1 && model.service_kinds[0] === 'llm' ? [] : (model.service_kinds ?? [])}
									<button
										class="flex flex-col gap-1 rounded-xl border border-border bg-card p-3 text-left shadow-card transition-colors hover:bg-card/80 {selected ? 'border-primary/50 bg-primary/5 text-primary' : ''}"
										onclick={() => toggle(model.id)}
									>
										<div class="flex items-start justify-between gap-2">
											<span class="break-all font-mono text-body-sm {model.id.startsWith('smart/') ? 'text-primary' : ''}">
												{model.id}
											</span>
											{#if selected}
												<CheckIcon class="size-4 shrink-0 text-primary" />
											{/if}
										</div>
										<div class="flex flex-wrap items-center gap-1.5">
											{#if model.owned_by && model.owned_by !== prefix}
												<span class="text-caption text-muted-foreground">{model.owned_by}</span>
											{/if}
											{#if model.id.startsWith('smart/')}
												<Badge variant="secondary" class="text-[10px] px-1.5 py-0 rounded-full">Smart</Badge>
											{/if}
											{#if kinds.length > 0}
												{#each kinds as kind (kind)}
													<Badge variant="outline" class="text-[10px] px-1.5 py-0 rounded-full">{kind}</Badge>
												{/each}
											{/if}
										</div>
									</button>
								{/each}
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>

		{#if multi}
			<div class="border-t border-border p-4">
				<Button class="w-full" onclick={confirm}>
					Done ({localSelection.size} selected)
				</Button>
			</div>
		{/if}
	</Dialog.Content>
</Dialog.Root>
