<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import * as Select from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { providersApi } from '$lib/api';
	import { toast } from 'svelte-sonner';
	import type { RoutingMode } from '$lib/api';

	let {
		open = $bindable(false),
		providerId,
		currentMode,
		currentFlatRate = false,
		onSaved,
	}: {
		open: boolean;
		providerId: string;
		currentMode: RoutingMode;
		currentFlatRate?: boolean;
		onSaved?: (mode: RoutingMode, flatRate: boolean) => void;
	} = $props();

	let selectedMode = $state<RoutingMode>("round_robin");
	let flatRate = $state(false);
	let submitting = $state(false);

	const modeOptions = [
		{ value: 'round_robin', label: 'Round robin' },
		{ value: 'random', label: 'Random' },
		{ value: 'first_eligible', label: 'First eligible' },
		{ value: 'affinity', label: 'Session affinity' },
	];

	$effect(() => {
		if (open) {
			selectedMode = currentMode;
			flatRate = currentFlatRate;
		}
	});

	async function handleSave() {
		submitting = true;
		try {
			await providersApi.updateSettings(providerId, { routing_mode: selectedMode, flat_rate: flatRate });
			toast.success('Provider settings saved');
			onSaved?.(selectedMode, flatRate);
			open = false;
		} catch (err) {
			toast.error('Failed to save provider settings: ' + (err instanceof Error ? err.message : 'Unknown'));
		} finally {
			submitting = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title class="text-display-md">Routing settings.</Dialog.Title>
			<Dialog.Description class="text-body-sm text-muted-foreground">
				Choose how requests are distributed across accounts for this provider.
			</Dialog.Description>
		</Dialog.Header>

		<div class="space-y-4 py-2">
			<div class="space-y-2">
				<Label class="text-body-sm">Routing mode</Label>
				<Select.Root type="single" bind:value={selectedMode}>
					<Select.Trigger class="w-full h-9 text-body-sm rounded-sm">
						{modeOptions.find((o) => o.value === selectedMode)?.label ?? 'Select mode'}
					</Select.Trigger>
					<Select.Content>
						{#each modeOptions as option}
							<Select.Item value={option.value} class="text-body-sm">{option.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>

			<div class="flex items-center justify-between rounded-md bg-card border border-border p-3">
				<div class="space-y-0.5">
					<Label class="text-body-sm">Flat-rate provider</Label>
					<p class="text-caption text-muted-foreground">Show $0 in dashboard cost analytics while tracking estimated cost internally.</p>
				</div>
				<Switch bind:checked={flatRate} />
			</div>

			<div class="rounded-md bg-accent/50 p-3 space-y-1">
				<p class="text-body-sm text-muted-foreground">
					<span class="font-medium text-foreground">Round robin</span> rotates across eligible accounts on every request.
				</p>
				<p class="text-body-sm text-muted-foreground">
					<span class="font-medium text-foreground">Random</span> picks one eligible account uniformly at random per request.
				</p>
				<p class="text-body-sm text-muted-foreground">
					<span class="font-medium text-foreground">First eligible</span> keeps one account until it cools down or exhausts.
				</p>
				<p class="text-body-sm text-muted-foreground">
					<span class="font-medium text-foreground">Session affinity</span> sticks repeat calls from the same Claude Code / Codex CLI session to the same account.
				</p>
			</div>
		</div>

		<Dialog.Footer>
			<Button variant="outline" onclick={() => (open = false)} class="text-body-sm rounded-sm">Cancel</Button>
			<Button onclick={handleSave} disabled={submitting} class="text-body-sm rounded-sm">
				{submitting ? 'Saving...' : 'Save'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
