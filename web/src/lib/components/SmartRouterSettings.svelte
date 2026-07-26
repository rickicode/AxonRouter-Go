<script lang="ts">
import { onMount } from 'svelte';
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Badge } from '$lib/components/ui/badge';
import { Skeleton } from '$lib/components/ui/skeleton';
import { Switch } from '$lib/components/ui/switch';
import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
import { toast } from 'svelte-sonner';
import SparklesIcon from '@lucide/svelte/icons/sparkles';
import XIcon from '@lucide/svelte/icons/x';
import {
	smartRouterApi,
	gatewayModelsApi,
	usageApi,
	computeVirtualModelTelemetry,
	defaultSmartRouterConfig,
	type SmartRouterConfig,
	type GatewayModel,
	type VirtualModelTelemetry,
} from '$lib/api';
import { formatLatency } from '$lib/stores';

let config = $state<SmartRouterConfig>(defaultSmartRouterConfig);
let loading = $state(true);
let saving = $state(false);
let candidateModels = $state<GatewayModel[]>([]);
let pickerOpen = $state(false);
let pickerTarget = $state<string>('');
let pickerExcluded = $state<string[]>([]);
let manualInputs = $state<Record<string, string>>({});
let usageByModel = $state<{ model_id?: string; requests: number; errors: number; avg_latency_ms: number; cost_usd: number; total_tokens: number }[]>([]);
let telemetry = $state<Record<string, VirtualModelTelemetry | null>>({});

const pickerModels = $derived(
	candidateModels.filter((m) => !pickerExcluded.includes(m.id)),
);

onMount(() => {
	loadAll();
});

async function loadAll() {
	loading = true;
	try {
		const [cfg, modelsRes, usageRes] = await Promise.all([
			smartRouterApi.getConfig(),
			gatewayModelsApi.list(),
			usageApi.get().catch(() => null),
		]);
		config = cfg;
		candidateModels = modelsRes.data.filter((m) => !m.id.startsWith('smart/'));
		usageByModel = usageRes?.data?.by_model ?? [];
		telemetry = computeTelemetryForAll(config, usageByModel);
	} catch (err) {
		toast.error('Failed to load smart router settings: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		loading = false;
	}
}

function computeTelemetryForAll(
	cfg: SmartRouterConfig,
	rows: { model_id?: string; requests: number; errors: number; avg_latency_ms: number; cost_usd: number; total_tokens: number }[],
): Record<string, VirtualModelTelemetry | null> {
	const out: Record<string, VirtualModelTelemetry | null> = {};
	for (const m of cfg.models) {
		out[m.id] = computeVirtualModelTelemetry(m.candidates, rows);
	}
	return out;
}

async function saveConfig(next: SmartRouterConfig) {
	saving = true;
	try {
		await smartRouterApi.updateConfig(next);
		config = next;
		telemetry = computeTelemetryForAll(config, usageByModel);
		toast.success('Smart router config saved');
	} catch (err) {
		toast.error('Failed to save smart router config: ' + (err instanceof Error ? err.message : 'Unknown'));
	} finally {
		saving = false;
	}
}

function toggleEnabled(id: string) {
	const next: SmartRouterConfig = {
		models: config.models.map((m) => (m.id === id ? { ...m, enabled: !m.enabled } : m)),
	};
	saveConfig(next);
}

function addCandidates(id: string, candidates: string[]) {
	const target = config.models.find((m) => m.id === id);
	if (!target) return;
	const set = new Set([...target.candidates, ...candidates]);
	const next: SmartRouterConfig = {
		models: config.models.map((m) =>
			m.id === id ? { ...m, candidates: Array.from(set) } : m,
		),
	};
	saveConfig(next);
}

function removeCandidate(id: string, candidate: string) {
	const next: SmartRouterConfig = {
		models: config.models.map((m) =>
			m.id === id ? { ...m, candidates: m.candidates.filter((c) => c !== candidate) } : m,
		),
	};
	saveConfig(next);
}

function addManualCandidate(id: string) {
	const raw = (manualInputs[id] ?? '').trim();
	if (!raw) return;
	if (!raw.includes('/')) {
		toast.error('Candidate model ID must be in provider/model-id format');
		return;
	}
	addCandidates(id, [raw]);
	manualInputs = { ...manualInputs, [id]: '' };
}

function openPicker(id: string) {
	pickerTarget = id;
	const target = config.models.find((m) => m.id === id);
	pickerExcluded = target?.candidates ?? [];
	pickerOpen = true;
}

function onPickerSelect(models: string[]) {
	if (pickerTarget && models.length > 0) {
		addCandidates(pickerTarget, models);
	}
	pickerTarget = '';
	pickerExcluded = [];
}

function formatSuccessRate(rate: number): string {
	return `${rate.toFixed(1)}%`;
}

function formatCostPer1K(value: number): string {
	return value === 0 ? '$0.0000' : `$${value.toFixed(4)}`;
}

function descriptionFor(id: string): string {
	if (id === 'smart/auto') return 'Dynamic routing using telemetry-weighted selection across candidates.';
	if (id === 'smart/auto-fast') return 'Prefer the lowest-latency candidate that is currently healthy.';
	return 'Prefer the highest-quality candidate regardless of cost.';
}
</script>

<div class="space-y-6">
	<Card class="shadow-card border-border/60">
		<CardHeader class="pb-3">
			<div class="flex items-center gap-2">
				<SparklesIcon class="size-5 text-primary" />
				<CardTitle class="text-body-md-strong">Smart Router</CardTitle>
			</div>
			<CardDescription class="text-body-sm">
				Manage virtual models that route requests to a candidate pool. Disabled
				virtual models are hidden from the model picker.
			</CardDescription>
		</CardHeader>
	</Card>

	{#if loading}
		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each Array(3) as _}
				<Card class="shadow-card border-border/60">
					<CardHeader class="pb-3">
						<Skeleton class="h-5 w-32" />
						<Skeleton class="mt-2 h-4 w-full" />
					</CardHeader>
					<CardContent class="space-y-3">
						<Skeleton class="h-8 w-full" />
						<Skeleton class="h-8 w-full" />
					</CardContent>
				</Card>
			{/each}
		</div>
	{:else}
		<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each config.models as vm (vm.id)}
				<Card class="shadow-card border-border/60 {vm.enabled ? '' : 'opacity-70'}">
					<CardHeader class="pb-3">
						<div class="flex items-start justify-between gap-4">
							<div class="min-w-0 space-y-1">
								<CardTitle class="text-body-md-strong flex items-center gap-2 flex-wrap">
									<code class="font-mono text-sm">{vm.id}</code>
									<Badge variant="secondary" class="text-[10px] px-1.5 py-0 rounded-full">Smart</Badge>
								</CardTitle>
								<CardDescription class="text-body-sm leading-relaxed">
									{descriptionFor(vm.id)}
								</CardDescription>
							</div>
							<Switch
								checked={vm.enabled}
								onCheckedChange={() => toggleEnabled(vm.id)}
								aria-label={vm.enabled ? `Disable ${vm.id}` : `Enable ${vm.id}`}
								disabled={saving}
							/>
						</div>
					</CardHeader>
					<CardContent class="space-y-5">
						<div class="space-y-2">
							<Label class="text-body-sm-strong">Candidate models</Label>
							{#if vm.candidates.length === 0}
								<p class="text-body-sm text-muted-foreground">
									No candidates configured. Requests to <code>{vm.id}</code> will fail until candidates are added.
								</p>
							{:else}
								<div class="flex flex-wrap gap-2">
									{#each vm.candidates as candidate (candidate)}
										<Badge variant="outline" class="gap-1 text-xs font-mono py-1 px-2">
											{candidate}
											<button
												type="button"
												class="text-muted-foreground hover:text-destructive disabled:opacity-50"
												onclick={() => removeCandidate(vm.id, candidate)}
												disabled={saving}
												aria-label={`Remove ${candidate}`}
											>
												<XIcon class="size-3" />
											</button>
										</Badge>
									{/each}
								</div>
							{/if}
							<div class="flex flex-col sm:flex-row gap-2 pt-1">
								<Button
									variant="outline"
									size="sm"
									class="text-body-sm rounded-sm"
									onclick={() => openPicker(vm.id)}
									disabled={saving || candidateModels.length === 0}
								>
									Add model
								</Button>
								<div class="flex gap-2 flex-1">
									<Input
										placeholder="provider/model-id"
										class="h-8 text-xs font-mono"
										bind:value={manualInputs[vm.id]}
										onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && addManualCandidate(vm.id)}
									/>
									<Button
										size="sm"
										class="text-body-sm rounded-sm"
										onclick={() => addManualCandidate(vm.id)}
										disabled={!(manualInputs[vm.id] ?? '').trim() || saving}
									>
										Add
									</Button>
								</div>
							</div>
						</div>

						<div class="rounded-lg border bg-muted/30 p-3 space-y-2">
							<p class="text-caption-mono uppercase tracking-wide text-muted-foreground">
								Telemetry (last 30 days)
							</p>
							{#if telemetry[vm.id]}
								{@const t = telemetry[vm.id]}
								<div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
									<div>
										<p class="text-xs text-muted-foreground">Requests</p>
										<p class="text-body-sm-strong">{t.requests.toLocaleString()}</p>
									</div>
									<div>
										<p class="text-xs text-muted-foreground">Avg latency</p>
										<p class="text-body-sm-strong">{formatLatency(t.avgLatencyMs)}</p>
									</div>
									<div>
										<p class="text-xs text-muted-foreground">Success rate</p>
										<p class="text-body-sm-strong">{formatSuccessRate(t.successRate)}</p>
									</div>
									<div>
										<p class="text-xs text-muted-foreground">Cost / 1K tokens</p>
										<p class="text-body-sm-strong">{formatCostPer1K(t.costPer1KTokens)}</p>
									</div>
								</div>
							{:else}
								<p class="text-body-sm text-muted-foreground">
									No usage data for configured candidates.
								</p>
							{/if}
						</div>
					</CardContent>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<ModelPickerDialog
	bind:open={pickerOpen}
	models={pickerModels}
	selectedModels={[]}
	onMultiSelect={onPickerSelect}
	multi={true}
/>
