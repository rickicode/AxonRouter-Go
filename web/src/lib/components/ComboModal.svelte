<script lang="ts">
import * as Dialog from '$lib/components/ui/dialog';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Switch } from '$lib/components/ui/switch';
import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
import { combosApi, modelsApi } from '$lib/api';
import type { Combo, ComboStep, GatewayModel } from '$lib/api';
import { unwrapStr } from '$lib/utils';
import { planStepSync, type StepDraft, type ExistingStep } from './combo-modal-helpers';
import { toast } from 'svelte-sonner';

let {
	open = $bindable(false),
	combo = null as Combo | null,
	onSave,
}: {
	open: boolean;
	combo: Combo | null;
	onSave?: (comboId: string) => void;
} = $props();

let name = $state('');
let strategy = $state('priority');
let timeout = $state(30000);
let stickyLimit = $state(1);
let isSmart = $state(false);
let smartGoal = $state('balanced');
let steps = $state<StepDraft[]>([]);
let existingSteps = $state<ExistingStep[]>([]);
let models = $state<GatewayModel[]>([]);
let pickerOpen = $state(false);
let loading = $state(false);
let stepsLoading = $state(false);

const strategyOptions = ['priority', 'round-robin', 'weighted'];
const smartGoalOptions = [
	{ value: 'auto', label: 'Auto', desc: 'Dynamic selection based on telemetry' },
	{ value: 'economy', label: 'Economy', desc: 'Lowest cost routing' },
	{ value: 'balanced', label: 'Balanced', desc: 'Cost, latency, quality balance' },
	{ value: 'premium', label: 'Premium', desc: 'Highest quality regardless of cost' },
];

function strategyLabel(opt: string) {
	if (opt === 'priority') return 'Priority';
	if (opt === 'round-robin') return 'Round Robin';
	return 'Weighted';
}

function strategyDescription(opt: string) {
	if (opt === 'priority') return 'Try steps in order. First success wins.';
	if (opt === 'round-robin') return 'Distribute requests across steps.';
	return 'Weighted-random order by step weight.';
}

function resetState() {
	if (combo) {
		name = combo.name;
		strategy = combo.strategy;
		timeout = combo.timeout_ms;
		stickyLimit = combo.sticky_limit;
    isSmart = combo.is_smart;
    smartGoal = unwrapStr(combo.smart_goal) ?? 'balanced';
    loadSteps(combo.id);
	} else {
		name = '';
		strategy = 'priority';
		timeout = 30000;
		stickyLimit = 0;
		isSmart = false;
		smartGoal = 'balanced';
		steps = [];
		existingSteps = [];
	}
	loadModels();
}

async function loadModels() {
	if (models.length > 0) return;
	try {
		const res = await modelsApi.list();
		models = res.data || [];
	} catch (err) {
		toast.error('Failed to load models: ' + (err instanceof Error ? err.message : 'Unknown'));
	}
}

async function loadSteps(comboId: string) {
	stepsLoading = true;
	try {
		const res = await combosApi.get(comboId);
		const normalized = (res.steps || []).map((s: ComboStep) => ({
			id: s.id,
			model_id: s.model_id,
			priority: s.priority,
			weight: s.weight,
		}));
		existingSteps = normalized;
		steps = normalized.map((s) => ({ ...s }));
	} catch (err) {
		toast.error('Failed to load steps: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		stepsLoading = false;
	}
}

$effect(() => {
	if (open) {
		resetState();
	}
});

function addModel(modelId: string) {
	if (steps.some((s) => s.model_id === modelId)) {
		toast.info('Model already added');
		return;
	}
	const nextPriority = steps.length > 0 ? Math.max(...steps.map((s) => s.priority)) + 1 : 1;
	steps = [...steps, { model_id: modelId, priority: nextPriority, weight: 100 }];
}

function removeStep(index: number) {
	steps = steps.filter((_, i) => i !== index);
}

function handleClose() {
	open = false;
}

async function handleSave() {
	if (!name.trim()) return;
	loading = true;
	try {
		if (combo) {
			await combosApi.update(combo.id, {
				name: name.trim(),
				strategy,
				timeout_ms: timeout,
				sticky_limit: stickyLimit,
				is_smart: isSmart,
				smart_goal: isSmart ? smartGoal : null,
			});
			const plan = planStepSync(existingSteps, steps);
			for (const stepId of plan.toRemove) {
				await combosApi.removeStep(stepId);
			}
			for (const step of plan.toAdd) {
				await combosApi.addStep(combo.id, step);
			}
			toast.success('Combo updated');
			onSave?.(combo.id);
		} else {
			const created = await combosApi.create({
				name: name.trim(),
				strategy,
				timeout_ms: timeout,
				sticky_limit: stickyLimit,
				is_smart: isSmart,
				smart_goal: isSmart ? smartGoal : null,
				is_active: true,
				steps: steps.map((s) => ({ model_id: s.model_id, priority: s.priority, weight: s.weight })),
			});
			toast.success('Combo created');
			onSave?.(created.id);
		}
		open = false;
	} catch (err) {
		toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		loading = false;
	}
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title class="text-body-md-strong">{combo ? 'Edit combo' : 'Create combo'}</Dialog.Title>
		</Dialog.Header>

		<div class="space-y-4 py-2">
			<div class="space-y-2">
				<Label class="text-body-sm-strong">Name</Label>
				<Input bind:value={name} placeholder="e.g. fallback, premium-rr" class="h-10 text-body-sm" />
			</div>

			<div class="space-y-2">
				<Label class="text-body-sm-strong">Strategy</Label>
				<div class="flex gap-2 flex-wrap">
					{#each strategyOptions as opt}
						<button
							class="cursor-pointer px-4 py-2 rounded-sm text-body-sm border transition-colors {strategy === opt ? 'bg-foreground text-background border-foreground' : 'border-border text-muted-foreground hover:text-foreground'}"
							onclick={() => (strategy = opt)}
						>
							{strategyLabel(opt)}
						</button>
					{/each}
				</div>
				<p class="text-caption text-muted-foreground">{strategyDescription(strategy)}</p>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-2">
					<Label class="text-body-sm-strong">Timeout</Label>
					<div class="flex items-center gap-1">
						<Input type="number" bind:value={timeout} class="h-10 text-code font-mono" />
						<span class="text-caption-mono text-muted-foreground whitespace-nowrap">ms</span>
					</div>
				</div>
				<div class="space-y-2">
					<Label class="text-body-sm-strong">Sticky limit</Label>
					<Input type="number" bind:value={stickyLimit} min={1} class="h-10 text-code font-mono" />
				</div>
			</div>

			<div class="flex items-center gap-3 pt-2 border-t border-border">
				<Switch id="combo-is-smart" checked={isSmart} onCheckedChange={(v) => (isSmart = v)} />
				<Label for="combo-is-smart" class="text-body-sm-strong cursor-pointer">Smart combo</Label>
			</div>

			{#if isSmart}
				<div class="space-y-2">
					<Label class="text-body-sm-strong">Goal</Label>
					<div class="grid grid-cols-2 gap-2">
						{#each smartGoalOptions as opt}
							<button
								class="cursor-pointer flex flex-col items-start gap-0.5 p-2.5 rounded-md border text-left transition-colors {smartGoal === opt.value ? 'border-foreground bg-accent' : 'border-border hover:border-foreground/50'}"
								onclick={() => (smartGoal = opt.value)}
							>
								<span class="text-body-sm-strong">{opt.label}</span>
								<span class="text-caption text-muted-foreground">{opt.desc}</span>
							</button>
						{/each}
					</div>
				</div>
			{/if}

			<Card class="shadow-card">
				<CardHeader class="pb-3">
					<div class="flex items-center justify-between">
						<CardTitle class="text-body-md-strong">Routing steps</CardTitle>
						<Button variant="outline" size="sm" onclick={() => (pickerOpen = true)} class="text-body-sm rounded-sm">
							Add model
						</Button>
					</div>
				</CardHeader>
				<CardContent>
					{#if stepsLoading}
						<div class="h-20 bg-muted animate-pulse rounded-md"></div>
					{:else if steps.length === 0}
						<div class="p-6 border border-dashed border-border rounded-md text-center">
							<p class="text-body-sm text-muted-foreground mb-1">No steps configured yet.</p>
							<p class="text-caption text-muted-foreground">Add models to define routing order.</p>
						</div>
					{:else}
						<div class="space-y-2">
							{#each steps as step, i (step.id ?? `${step.model_id}-${i}`)}
								<div class="flex items-center gap-3 p-2.5 border border-border rounded-md bg-card/50">
									<span class="flex-1 min-w-0 text-body-sm font-mono truncate">{step.model_id}</span>
									<div class="flex items-center gap-2">
										<div class="space-y-1">
											<Label class="text-caption text-muted-foreground">Priority</Label>
											<Input type="number" bind:value={step.priority} class="h-8 w-20 text-code font-mono" />
										</div>
										<div class="space-y-1">
											<Label class="text-caption text-muted-foreground">Weight</Label>
											<Input type="number" bind:value={step.weight} class="h-8 w-20 text-code font-mono" />
										</div>
									</div>
									<Button variant="ghost" size="sm" onclick={() => removeStep(i)} class="text-caption-mono text-destructive h-8 px-2 rounded-sm">
										Remove
									</Button>
								</div>
							{/each}
						</div>
					{/if}
				</CardContent>
			</Card>
		</div>

		<Dialog.Footer>
			<Button variant="ghost" onclick={handleClose}>Cancel</Button>
			<Button onclick={handleSave} disabled={loading || !name.trim()}>
				{loading ? 'Saving...' : 'Save'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<ModelPickerDialog bind:open={pickerOpen} {models} onSelect={addModel} />
