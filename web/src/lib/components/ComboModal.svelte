<script lang="ts">
import * as Dialog from '$lib/components/ui/dialog';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Switch } from '$lib/components/ui/switch';
import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
import { ScrollArea } from '$lib/components/ui/scroll-area';
import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
import { combosApi, modelsApi } from '$lib/api';
import type { Combo, ComboStep, GatewayModel } from '$lib/api';
import { unwrapStr } from '$lib/utils';
import { planStepSync, type StepDraft, type ExistingStep } from './combo-modal-helpers';
import { toast } from 'svelte-sonner';
import ChevronUpIcon from '@lucide/svelte/icons/chevron-up';
import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

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
let judgeModel = $state('');
let minPanel = $state(2);
let stragglerGraceMs = $state(8000);
let panelHardTimeoutMs = $state(90000);
let anonymizeSources = $state(true);
let steps = $state<StepDraft[]>([]);
let existingSteps = $state<ExistingStep[]>([]);
let models = $state<GatewayModel[]>([]);
let pickerOpen = $state(false);
let judgePickerOpen = $state(false);
let loading = $state(false);
let stepsLoading = $state(false);

	const strategyOptions = ['priority', 'round-robin', 'weighted', 'random', 'least-used', 'fusion'];
const smartGoalOptions = [
	{ value: 'auto', label: 'Auto', desc: 'Dynamic selection based on telemetry' },
	{ value: 'economy', label: 'Economy', desc: 'Lowest cost routing' },
	{ value: 'balanced', label: 'Balanced', desc: 'Cost, latency, quality balance' },
	{ value: 'premium', label: 'Premium', desc: 'Highest quality regardless of cost' },
];

function strategyLabel(opt: string) {
	if (opt === 'priority') return 'Priority';
	if (opt === 'round-robin') return 'Round Robin';
	if (opt === 'random') return 'Random';
	if (opt === 'least-used') return 'Least Used';
	if (opt === 'fusion') return 'Fusion';
	return 'Weighted';
}

function strategyDescription(opt: string) {
	if (opt === 'priority') return 'Try steps in order. First success wins.';
	if (opt === 'round-robin') return 'Rotate to a different step each request.';
	if (opt === 'random') return 'Pick a random step each request.';
	if (opt === 'least-used') return 'Prefer the model with the fewest recent successful calls.';
	if (opt === 'fusion') return 'Parallel panel + judge synthesis.';
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
		loadFusionConfig(combo.fusion_config);
		loadSteps(combo.id);
	} else {
		name = '';
		strategy = 'priority';
		timeout = 30000;
		stickyLimit = 1;
		isSmart = false;
		smartGoal = 'balanced';
		resetFusionConfig();
		steps = [];
		existingSteps = [];
	}
	loadModels();
}

function resetFusionConfig() {
	judgeModel = '';
	minPanel = 2;
	stragglerGraceMs = 8000;
	panelHardTimeoutMs = 90000;
	anonymizeSources = true;
}

function loadFusionConfig(raw: string | null | undefined) {
	resetFusionConfig();
	if (!raw) return;
	try {
		const cfg = JSON.parse(raw);
		if (cfg.judge_model !== undefined) judgeModel = cfg.judge_model;
		if (cfg.min_panel !== undefined) minPanel = cfg.min_panel;
		if (cfg.straggler_grace_ms !== undefined) stragglerGraceMs = cfg.straggler_grace_ms;
		if (cfg.panel_hard_timeout_ms !== undefined) panelHardTimeoutMs = cfg.panel_hard_timeout_ms;
		if (cfg.anonymize_sources !== undefined) anonymizeSources = cfg.anonymize_sources;
	} catch {
		// ignore malformed fusion_config
	}
}

function buildFusionConfig(): string {
	return JSON.stringify({
		judge_model: judgeModel || undefined,
		min_panel: minPanel,
		straggler_grace_ms: stragglerGraceMs,
		panel_hard_timeout_ms: panelHardTimeoutMs,
		anonymize_sources: anonymizeSources,
	});
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

const fusionStrategy = $derived(strategy === 'fusion');

$effect(() => {
	if (fusionStrategy && isSmart) {
		isSmart = false;
	}
});

function addModel(modelId: string) {
  if (steps.some((s) => s.model_id === modelId)) {
    toast.info('Model already added');
    return;
  }
  commitSteps([...steps, { model_id: modelId, priority: steps.length + 1, weight: 100 }]);
}


function commitSteps(next: StepDraft[]) {
  steps = next.map((s, i) => ({ ...s, priority: i + 1 }));
}

function removeStep(index: number) {
  commitSteps(steps.filter((_, i) => i !== index));
}

function moveStepUp(index: number) {
  if (index <= 0) return;
  const next = [...steps];
  [next[index - 1], next[index]] = [next[index], next[index - 1]];
  commitSteps(next);
}

function moveStepDown(index: number) {
  if (index >= steps.length - 1) return;
  const next = [...steps];
  [next[index], next[index + 1]] = [next[index + 1], next[index]];
  commitSteps(next);
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
		is_smart: fusionStrategy ? false : isSmart,
		smart_goal: fusionStrategy || !isSmart ? null : smartGoal,
		fusion_config: strategy === 'fusion' ? buildFusionConfig() : undefined,
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
			is_smart: fusionStrategy ? false : isSmart,
			smart_goal: fusionStrategy || !isSmart ? null : smartGoal,
			fusion_config: strategy === 'fusion' ? buildFusionConfig() : undefined,
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
  <Dialog.Content class="sm:max-w-2xl max-h-[85vh] flex flex-col">
    <Dialog.Header>
    <Dialog.Title class="text-body-md-strong">{combo ? 'Edit combo' : 'Create combo'}</Dialog.Title>
  </Dialog.Header>

  <div class="flex-1 overflow-y-auto min-h-0">
    <div class="space-y-4 py-2">

			<div class="space-y-2">
				<Label class="text-body-sm-strong">Name</Label>
				<Input bind:value={name} placeholder="e.g. random, premium-rr" class="h-10 text-body-sm" />
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

		{#if strategy === 'fusion'}
		<div class="space-y-3 border border-border rounded-md p-3 bg-card/50">
			<p class="text-body-sm-strong">Fusion panel configuration</p>
			<div class="space-y-2">
				<Label class="text-caption-mono">Judge model</Label>
				{#if judgeModel}
					<div class="flex items-center justify-between gap-2 rounded-md border border-border bg-muted/30 px-3 py-2">
						<span class="text-body-sm font-mono truncate">{judgeModel}</span>
						<Button variant="ghost" size="sm" class="h-7 rounded-sm text-caption-mono" onclick={() => (judgeModel = '')}>Clear</Button>
					</div>
				{:else}
					<Button variant="outline" size="sm" class="text-body-sm rounded-sm w-full justify-start" onclick={() => (judgePickerOpen = true)}>
						Select judge model
					</Button>
				{/if}
				<p class="text-caption text-muted-foreground">Leave empty to use the first successful panel response as the judge.</p>
			</div>
					<div class="grid grid-cols-3 gap-3">
						<div class="space-y-1">
							<Label class="text-caption-mono">Min panel</Label>
							<Input type="number" bind:value={minPanel} min={1} class="h-10 text-code font-mono" />
						</div>
						<div class="space-y-1">
							<Label class="text-caption-mono">Grace (ms)</Label>
							<Input type="number" bind:value={stragglerGraceMs} min={0} class="h-10 text-code font-mono" />
						</div>
						<div class="space-y-1">
							<Label class="text-caption-mono">Hard timeout (ms)</Label>
							<Input type="number" bind:value={panelHardTimeoutMs} min={1000} class="h-10 text-code font-mono" />
						</div>
					</div>
					<div class="flex items-center gap-3 pt-1">
						<Switch id="combo-anonymize" checked={anonymizeSources} onCheckedChange={(v) => (anonymizeSources = v)} />
						<Label for="combo-anonymize" class="text-body-sm-strong cursor-pointer">Anonymize panel sources</Label>
					</div>
				</div>
			{/if}

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
		<Switch id="combo-is-smart" checked={isSmart} onCheckedChange={(v) => (isSmart = v)} disabled={fusionStrategy} />
		<Label for="combo-is-smart" class="text-body-sm-strong {fusionStrategy ? 'text-muted-foreground cursor-not-allowed' : 'cursor-pointer'}">Smart combo</Label>
	</div>
	{#if fusionStrategy}
		<p class="text-caption text-muted-foreground">Smart routing is not available for fusion combos because fusion already uses its own panel + judge.</p>
	{/if}

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
				<ScrollArea class="max-h-[40vh]">
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
	                  <div class="flex flex-col gap-0.5">
	                    <Button
	                      variant="outline"
	                      size="icon"
	                      class="size-7 rounded-sm"
	                      disabled={i === 0}
	                      onclick={() => moveStepUp(i)}
	                      aria-label="Move up"
	                      title="Move up"
	                    >
	                      <ChevronUpIcon class="size-4" />
	                    </Button>
	                    <Button
	                      variant="outline"
	                      size="icon"
	                      class="size-7 rounded-sm"
	                      disabled={i === steps.length - 1}
	                      onclick={() => moveStepDown(i)}
	                      aria-label="Move down"
	                      title="Move down"
	                    >
	                      <ChevronDownIcon class="size-4" />
	                    </Button>
	                  </div>
	                  <span class="flex-1 min-w-0 text-body-sm font-mono truncate">{step.model_id}</span>
	                  <div class="flex items-center gap-2">
					<div class="space-y-1">
						<Label class="text-caption text-muted-foreground">Order</Label>
						<span class="flex h-8 w-16 items-center justify-center rounded-md border border-border bg-muted/30 text-code font-mono">{i + 1}</span>
					</div>
					{#if strategy === 'weighted'}
						<div class="space-y-1">
							<Label class="text-caption text-muted-foreground">Weight</Label>
							<Input type="number" bind:value={step.weight} class="h-8 w-20 text-code font-mono" />
						</div>
					{/if}
	                  </div>
	                  <Button variant="ghost" size="sm" onclick={() => removeStep(i)} class="text-caption-mono text-destructive h-8 px-2 rounded-sm">
	                    Remove
	                  </Button>
	                </div>
	              {/each}
	            </div>
	
						{/if}
					</CardContent>
				</ScrollArea>
    </Card>
    </div>
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
<ModelPickerDialog bind:open={judgePickerOpen} {models} selectedModel={judgeModel} onSelect={(id) => (judgeModel = id)} />
