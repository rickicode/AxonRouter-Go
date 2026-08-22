<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
	import { visionBridgeApi } from '$lib/api';
	import type { GatewayModel } from '$lib/api';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import Loader2Icon from '@lucide/svelte/icons/loader-2';
	import CheckIcon from '@lucide/svelte/icons/check';
	import XIcon from '@lucide/svelte/icons/x';

	let configuredModel = $state('');
	let models = $state<GatewayModel[]>([]);
	let pickerOpen = $state(false);
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);

	onMount(load);

	async function load() {
		loading = true;
		error = null;
		try {
			const [model, available] = await Promise.all([
				visionBridgeApi.getModel(),
				visionBridgeApi.models(),
			]);
			configuredModel = model;
			models = available.data || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load Vision Bridge settings';
		} finally {
			loading = false;
		}
	}

	async function selectModel(model: string) {
		saving = true;
		try {
			await visionBridgeApi.setModel(model);
			configuredModel = model;
			pickerOpen = false;
			toast.success('Vision Bridge model saved');
		} catch (err) {
			toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
		} finally {
			saving = false;
		}
	}

	async function disable() {
		saving = true;
		try {
			await visionBridgeApi.disable();
			configuredModel = '';
			toast.success('Vision Bridge disabled');
		} catch (err) {
			toast.error('Disable failed: ' + (err instanceof Error ? err.message : 'Unknown'));
		} finally {
			saving = false;
		}
	}
</script>

<Card class="shadow-card border-border/60">
	<CardHeader class="pb-3">
		<div class="flex items-start gap-3">
			<span class="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
				<EyeIcon class="size-5" />
			</span>
			<div class="flex-1">
				<CardTitle class="text-body-md-strong">Vision Bridge</CardTitle>
				<CardDescription class="text-body-sm">
					Let text-only models understand images through a configured vision model.
				</CardDescription>
			</div>
			{#if configuredModel}
				<Badge variant="default" class="gap-1">
					<CheckIcon class="size-3" />
					Enabled
				</Badge>
			{:else}
				<Badge variant="secondary" class="gap-1">
					<XIcon class="size-3" />
					Disabled
				</Badge>
			{/if}
		</div>
	</CardHeader>

	<CardContent class="space-y-4">
		<p class="text-body-sm text-muted-foreground">
			When the selected target model does not support vision, AxonRouter sends the image to the bridge model first, replaces it with the returned description, and then forwards the enriched request. This adds one upstream request per image turn.
		</p>

		{#if loading}
			<div class="flex items-center gap-2 py-4 text-body-sm text-muted-foreground">
				<Loader2Icon class="size-4 animate-spin" />
				Loading vision models…
			</div>
		{:else if error}
			<div class="flex flex-col items-start gap-3 py-2">
				<p class="text-body-sm text-destructive">{error}</p>
				<Button onclick={load} variant="outline" size="sm" class="text-body-sm rounded-sm">Try again</Button>
			</div>
		{:else}
			<div class="space-y-2">
				<p class="text-caption-mono text-muted-foreground uppercase">Configured model</p>
				{#if configuredModel}
					<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between rounded-xl border border-border bg-card p-3">
						<span class="min-w-0 break-all font-mono text-body-sm">{configuredModel}</span>
						<div class="flex shrink-0 gap-2">
							<Button onclick={() => (pickerOpen = true)} variant="outline" size="sm" class="text-body-sm rounded-sm" disabled={saving}>Change</Button>
							<Button onclick={disable} variant="destructive" size="sm" class="text-body-sm rounded-sm" disabled={saving}>Disable</Button>
						</div>
					</div>
				{:else}
					<div class="flex flex-col items-start gap-3 rounded-xl border border-dashed border-border bg-card p-4">
						<p class="text-body-sm text-muted-foreground">No vision model configured. Image requests will use their normal routing behavior.</p>
						<Button onclick={() => (pickerOpen = true)} size="sm" class="text-body-sm rounded-sm" disabled={saving || models.length === 0}>Select vision model</Button>
					</div>
				{/if}
			</div>

			{#if models.length === 0}
				<p class="text-caption text-muted-foreground">No active model with a known vision capability is available. Add or enable a provider connection first.</p>
			{/if}
		{/if}
	</CardContent>
</Card>

<ModelPickerDialog
	bind:open={pickerOpen}
	models={models}
	selectedModel={configuredModel}
	onSelect={(model) => selectModel(model)}
/>
