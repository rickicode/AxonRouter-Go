<script lang="ts">
 import * as Dialog from '$lib/components/ui/dialog';
 import { Button } from '$lib/components/ui/button';
 import { Input } from '$lib/components/ui/input';
 import { Label } from '$lib/components/ui/label';
 import { providersApi } from '$lib/api';
 import { toast } from 'svelte-sonner';

let {
	open = $bindable(false),
	providerId,
	currentBaseUrl = '',
	currentDisplayName = '',
	currentServiceKinds = [],
	currentFormat = 'openai',
	onSaved,
}: {
	open: boolean;
	providerId: string;
	currentBaseUrl?: string;
	currentDisplayName?: string;
	currentServiceKinds?: string[];
	currentFormat?: string;
	onSaved?: () => void;
} = $props();

let baseUrl = $state('');
let displayName = $state('');
let selectedServiceKinds = $state<string[]>([]);
let submitting = $state(false);

const SERVICE_KIND_OPTIONS: { id: string; label: string }[] = [
	{ id: 'llm', label: 'LLM' },
	{ id: 'embedding', label: 'Embeddings' },
	{ id: 'image', label: 'Images' },
];

$effect(() => {
	if (open) {
		baseUrl = currentBaseUrl ?? '';
		displayName = currentDisplayName ?? '';
		selectedServiceKinds = currentServiceKinds?.length ? [...currentServiceKinds] : ['llm'];
	}
});

function toggleServiceKind(id: string) {
	if (selectedServiceKinds.includes(id)) {
		if (selectedServiceKinds.length > 1) {
			selectedServiceKinds = selectedServiceKinds.filter((k) => k !== id);
		}
	} else {
		selectedServiceKinds = [...selectedServiceKinds, id];
	}
}

async function handleSave() {
	if (!baseUrl.trim()) {
		toast.error('Base URL is required');
		return;
	}
	submitting = true;
	try {
		const payload: Record<string, unknown> = {
			base_url: baseUrl.trim(),
			display_name: displayName.trim(),
		};
		if (currentFormat === 'openai') {
			payload.service_kinds = selectedServiceKinds;
		}
		await providersApi.update(providerId, payload);
 toast.success('Provider updated');
 onSaved?.();
 open = false;
 } catch (err) {
 toast.error('Failed to update provider: ' + (err instanceof Error ? err.message : 'Unknown'));
 } finally {
 submitting = false;
 }
 }
</script>

<Dialog.Root bind:open>
 <Dialog.Content class="sm:max-w-md">
 <Dialog.Header>
 <Dialog.Title class="text-display-md">Edit provider.</Dialog.Title>
 <Dialog.Description class="text-body-sm text-muted-foreground">
 Update the base URL and display name for this custom provider.
 </Dialog.Description>
 </Dialog.Header>

	<div class="space-y-4 py-2">
		<div class="space-y-2">
			<Label class="text-body-sm">Display name</Label>
			<Input bind:value={displayName} placeholder="Display name" class="h-9 text-body-sm rounded-sm" />
		</div>
		<div class="space-y-2">
			<Label class="text-body-sm">Base URL</Label>
			<Input bind:value={baseUrl} placeholder="https://..." class="h-9 text-body-sm rounded-sm font-mono" />
			<p class="text-caption text-muted-foreground">The OpenAI-compatible endpoint this provider proxies to (e.g. https://api.example.com/v1).</p>
		</div>
		{#if currentFormat === 'openai'}
			<div class="space-y-2">
				<Label class="text-body-sm">Service kinds</Label>
				<div class="flex flex-wrap gap-2">
					{#each SERVICE_KIND_OPTIONS as opt}
						<button
							type="button"
							onclick={() => toggleServiceKind(opt.id)}
							class="rounded-full border px-2.5 py-1 text-xs font-medium transition-colors
							{selectedServiceKinds.includes(opt.id)
								? 'border-foreground bg-foreground text-background'
								: 'border-border text-muted-foreground hover:border-foreground/30 hover:text-foreground'}"
						>
							{opt.label}
						</button>
					{/each}
				</div>
			</div>
		{/if}
	</div>

 <Dialog.Footer>
 <Button variant="outline" onclick={() => (open = false)} class="text-body-sm rounded-sm">Cancel</Button>
 <Button onclick={handleSave} disabled={submitting} class="text-body-sm rounded-sm">
 {submitting ? 'Saving...' : 'Save'}
 </Button>
 </Dialog.Footer>
 </Dialog.Content>
</Dialog.Root>
