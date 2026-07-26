<script lang="ts">
import { onMount } from 'svelte';
import { router } from '$lib/router';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Textarea } from '$lib/components/ui/textarea';
import { Badge } from '$lib/components/ui/badge';
import { Switch } from '$lib/components/ui/switch';
import * as Select from '$lib/components/ui/select';
import { Skeleton } from '$lib/components/ui/skeleton';
import { toast } from 'svelte-sonner';
import { copyToClipboard } from '$lib/copy';
import SearchIcon from '@lucide/svelte/icons/search';
import InfoIcon from '@lucide/svelte/icons/info';
import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
import XCircleIcon from '@lucide/svelte/icons/x-circle';
import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
import { cliToolsApi, modelsApi, apiKeysApi } from '$lib/api';
import ModelPickerDialog from '$lib/components/ModelPickerDialog.svelte';
import { CLIConfigOutput } from '$lib/components/cli-tools';
import type {
	CLITool,
	GatewayModel,
	APIKeyItem,
	CLIToolSelection,
	CLIToolConfig,
	CLIToolState,
} from '$lib/api';

let { id = '' }: { id?: string } = $props();

// Page state
let tool = $state<CLITool | null>(null);
let models = $state<GatewayModel[]>([]);
let keys = $state<APIKeyItem[]>([]);
let loading = $state(true);
let pageError = $state<string | null>(null);

// Tool state
let sel = $state<CLIToolSelection>({
	model: '',
	apiKeyId: '',
	baseUrl: '',
	models: [],
	useDiscovery: false,
	activeModel: '',
	subagentModel: '',
	agentModels: {},
});
let detailInstalled = $state(false);
let detailHasRouter = $state(false);
let detailState = $state<unknown>(null);
let detailConfigured = $state(false);
let detailConfig = $state<CLIToolConfig | null>(null);
let generated = $state<CLIToolConfig | null>(null);
let generating = $state(false);
let resetting = $state(false);
let copiedField = $state<string | null>(null);
let modelAliases = $state<Record<string, string>>({});

// Model picker
let modelPickerOpen = $state(false);
let modelPickerTarget = $state<string>('_main');
let modelPickerMulti = $state(false);

const defaultBaseUrl =
	typeof window !== 'undefined' ? `${window.location.origin}/v1` : 'http://localhost:3777/v1';

onMount(() => {
	document.title = 'CLI Tool — AxonRouter';
	loadAll();
});

async function loadAll() {
	loading = true;
	pageError = null;
	try {
		const [toolRes, modelsRes, keysRes] = await Promise.all([
			cliToolsApi.get(id),
			modelsApi.list(),
			apiKeysApi.list(),
		]);
		const res = toolRes as CLIToolState;
		tool = res.tool ?? null;
		models = modelsRes.data ?? [];
		keys = keysRes.data ?? [];
		const s = res.selection ?? ({} as CLIToolSelection);
		detailInstalled = res.installed ?? false;
		detailHasRouter = res.hasRouter ?? false;
		detailState = res.state ?? null;
		detailConfigured = res.configured ?? false;
		detailConfig = res.config ?? null;
		sel = {
			model: s.model ?? '',
			apiKeyId: s.apiKeyId ?? '',
			baseUrl: s.baseUrl || res.defaultBaseUrl || defaultBaseUrl,
			models: s.models ?? [],
			useDiscovery: s.useDiscovery ?? false,
			activeModel: s.activeModel ?? '',
			subagentModel: s.subagentModel ?? '',
			agentModels: s.agentModels ?? {},
		};
		if (s.modelAliases) {
			modelAliases = { ...s.modelAliases };
		}
		generated = res.config ?? null;
		// Initialize alias defaults for tools with defaultModels
		if (tool?.defaultModels) {
			for (const dm of tool.defaultModels) {
				if (!modelAliases[dm.alias] && dm.defaultValue) {
					modelAliases[dm.alias] = dm.defaultValue;
				}
			}
		}
	} catch (err) {
		pageError = err instanceof Error ? err.message : 'Failed to load CLI tool';
		toast.error(pageError);
	} finally {
		loading = false;
	}
}

async function applyConfig() {
	if (!tool) return;
	generating = true;
	try {
		const res = await cliToolsApi.save(tool.id, {
			...sel,
			modelAliases: Object.keys(modelAliases).length > 0 ? modelAliases : undefined,
		});
		sel = res.selection;
		generated = res.config;
		detailConfig = res.config;
		detailConfigured = true;
		detailHasRouter = true;
		const path = res.config?.configPath;
		toast.success(`Generated config for ${tool.name}${path ? ' → ' + path : ''}`);
		setTimeout(
			() =>
				document.getElementById('generated-section')?.scrollIntoView({
					behavior: 'smooth',
					block: 'start',
				}),
			50,
		);
	} catch (err) {
		toast.error(err instanceof Error ? err.message : 'Failed to generate config');
	} finally {
		generating = false;
	}
}

async function resetConfig() {
	if (!tool) return;
	resetting = true;
	try {
		await cliToolsApi.delete(tool.id);
		generated = null;
		detailConfig = null;
		detailConfigured = false;
		detailHasRouter = false;
		toast.success(`Reset ${tool.name} configuration`);
	} catch (err) {
		toast.error(err instanceof Error ? err.message : 'Failed to reset configuration');
	} finally {
		resetting = false;
	}
}

async function copyText(text: string, field: string) {
	if (!text) return;
	const label = field === 'env' ? 'Environment variables' : 'Config file';
	const ok = await copyToClipboard(text, label);
	if (ok) {
		copiedField = field;
		setTimeout(() => (copiedField = null), 2000);
	}
}

function openModelPicker(target: string, multi = false) {
	modelPickerTarget = target;
	modelPickerMulti = multi;
	modelPickerOpen = true;
}

function onModelPick(modelId: string) {
	if (modelPickerTarget === '_main') {
		sel.model = modelId;
	} else if (modelPickerTarget === 'activeModel') {
		sel.activeModel = modelId;
	} else if (modelPickerTarget === 'subagentModel') {
		sel.subagentModel = modelId;
	} else {
		modelAliases[modelPickerTarget] = modelId;
	}
}

function onMultiSelect(modelIds: string[]) {
	if (modelPickerTarget === '_main') {
		sel.models = modelIds;
	}
}

function addModel() {
	const m = sel.model.trim();
	if (!m) return;
	if (!sel.models?.includes(m)) {
		sel.models = [...(sel.models || []), m];
	}
	sel.model = '';
}

function removeModel(index: number) {
	sel.models = (sel.models || []).filter((_, i) => i !== index);
}

// Template variable substitution for code blocks and guide step values
function replaceVars(text: string): string {
	const key = '__YOUR_AXONROUTER_API_KEY__';
	const base = sel.baseUrl
		? sel.baseUrl.endsWith('/v1')
			? sel.baseUrl
			: `${sel.baseUrl}/v1`
		: defaultBaseUrl;
	const model = sel.models?.[0] || sel.model || getFirstAliasModel() || 'provider/model-id';
	return text
		.replace(/\{\{baseUrl\}\}/g, base)
		.replace(/\{\{apiKey\}\}/g, key)
		.replace(/\{\{model\}\}/g, model);
}

function getFirstAliasModel(): string {
	const vals = Object.values(modelAliases).filter(Boolean);
	return vals.length > 0 ? vals[0] : '';
}

function getEffectiveModel(): string {
	return sel.model || getFirstAliasModel() || '';
}

function getDetailBadge() {
	if (!detailInstalled) {
		if (detailConfigured) {
			return {
				label: 'Configured',
				cls: 'bg-sky-500/10 text-sky-400 border-sky-500/20',
			};
		}
		return {
			label: 'Not configured',
			cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20',
		};
	}
	if (detailHasRouter) {
		return {
			label: 'Connected',
			cls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
		};
	}
	if (detailConfigured) {
		return {
			label: 'Configured',
			cls: 'bg-sky-500/10 text-sky-400 border-sky-500/20',
		};
	}
	return {
		label: 'Not configured',
		cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20',
	};
}

function getNoteIcon(type: string) {
	if (type === 'warning') return AlertTriangleIcon;
	if (type === 'error') return XCircleIcon;
	return InfoIcon;
}

function getNoteColors(type: string) {
	if (type === 'warning') return 'border-yellow-500/30 bg-yellow-500/10 text-yellow-400';
	if (type === 'error') return 'border-red-500/30 bg-red-500/10 text-red-400';
	return 'border-blue-500/30 bg-blue-500/10 text-blue-400';
}

function showsSubagentModel(t: CLITool | null): boolean {
	return !!t && (t.id === 'codex' || t.id === 'opencode' || t.id === 'droid');
}

function showsActiveModel(t: CLITool | null): boolean {
	return !!t && (t.id === 'opencode' || t.id === 'droid');
}

function goBack() {
	router.navigate('/cli-tools');
}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	{#if loading}
		<div class="flex flex-col gap-6">
			<div class="h-8 w-64 bg-muted animate-pulse rounded-md"></div>
			<div class="h-40 bg-muted animate-pulse rounded-md"></div>
		</div>
	{:else if pageError}
		<div class="rounded-xl border border-destructive/30 bg-destructive/10 p-4 text-body-sm text-destructive">
			{pageError}
		</div>
	{/if}

	{#if tool}
		<div class="flex items-start gap-4">
			<Button variant="outline" size="sm" class="gap-1.5 shrink-0" onclick={goBack}>
				<ArrowLeftIcon class="size-4" />
				Back
			</Button>
			<div class="flex size-12 shrink-0 items-center justify-center rounded-lg bg-background/50">
				<img
					src={tool.image}
					alt={tool.name}
					class="size-10 rounded-lg object-contain"
					onerror={(e) => (e.currentTarget.style.display = 'none')}
				/>
			</div>
			<div class="min-w-0 flex-1 space-y-1">
				<div class="flex flex-wrap items-center gap-2">
					<h1 class="text-display-lg">{tool.name}.</h1>
					<Badge variant="outline" class="border-0 {getDetailBadge().cls}">
						{getDetailBadge().label}
					</Badge>
				</div>
				<p class="text-body-sm text-muted-foreground">{tool.description}</p>
			</div>
		</div>

		<div class="grid grid-cols-1 gap-6 lg:grid-cols-[1fr_22rem]">
			<div class="flex flex-col gap-6">
				<!-- Connection hints -->
				{#if !detailInstalled}
					<div
						class="flex items-start gap-2.5 rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-yellow-400"
					>
						<AlertTriangleIcon class="size-4 shrink-0 mt-0.5" />
						<p class="text-body-sm">
							{tool.name} is not detected on this machine. You can still generate a manual
							config if AxonRouter runs on a remote server.
						</p>
					</div>
				{:else if detailInstalled && !detailHasRouter}
					<div
						class="flex items-start gap-2.5 rounded-lg border border-sky-500/30 bg-sky-500/10 p-3 text-sky-400"
					>
						<InfoIcon class="size-4 shrink-0 mt-0.5" />
						<p class="text-body-sm">
							{tool.name} is installed but not yet connected to AxonRouter. Apply a config to wire
							it up.
						</p>
					</div>
				{/if}

				{#if tool.docsUrl}
					<a
						href={tool.docsUrl}
						target="_blank"
						rel="noopener noreferrer"
						class="inline-flex items-center gap-1.5 text-caption text-muted-foreground hover:text-primary"
					>
						<ExternalLinkIcon class="size-3" /> Documentation
					</a>
				{/if}

				<!-- Notes -->
				{#if tool.notes?.length}
					{#each tool.notes as note}
						{@const Icon = getNoteIcon(note.type)}
						<div class="flex items-start gap-2.5 rounded-lg border p-3 {getNoteColors(note.type)}">
							<Icon class="size-4 shrink-0 mt-0.5" />
							<p class="text-body-sm">{note.text}</p>
						</div>
					{/each}
				{/if}

				<!-- Guide steps -->
				{#if (tool.guideSteps?.length ?? 0) > 0}
					<div class="space-y-3">
						{#each tool.guideSteps as step}
							<div class="flex gap-3">
								<div
									class="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary text-[11px] font-bold"
								>
									{step.step}
								</div>
								<div class="min-w-0 flex-1 space-y-1.5">
									<p class="text-body-sm-strong">{step.title}</p>
									{#if step.desc}
										<p class="text-body-sm text-muted-foreground">{step.desc}</p>
									{/if}
									{#if step.value && step.copyable}
										<code
											class="block rounded-md border border-border bg-background px-2.5 py-1.5 font-mono text-body-sm"
											>{replaceVars(step.value)}</code
										>
									{/if}
									{#if step.type === 'modelSelector'}
										<div class="space-y-3">
											{#if tool?.supportsDiscovery}
												<div class="flex items-center gap-2">
													<Switch id="auto-discovery" bind:checked={sel.useDiscovery} />
													<Label
														for="auto-discovery"
														class="text-body-sm cursor-pointer"
													>
														Auto-discover models from gateway
													</Label>
												</div>
											{/if}
											{#if !sel.useDiscovery || !tool?.supportsDiscovery}
												<div class="flex flex-wrap gap-2">
													{#each sel.models || [] as m, i (m)}
														<div
															class="flex items-center gap-1 rounded-md border border-border px-2 py-1 font-mono text-caption"
														>
															<span class="max-w-[200px] truncate">{m}</span>
															<button
																class="text-muted-foreground hover:text-foreground cursor-pointer"
																onclick={() => removeModel(i)}
															>
																<XCircleIcon class="size-3" />
															</button>
														</div>
													{/each}
												</div>
												<div class="flex gap-2">
													<Input
														bind:value={sel.model}
														placeholder="provider/model-id"
														class="font-mono text-body-sm flex-1"
													/>
													<Button
														variant="outline"
														size="sm"
														class="gap-1.5"
														onclick={() => openModelPicker('_main', true)}
														disabled={models.length === 0}
														>
															<SearchIcon class="size-3.5" /> Browse
														</Button>
													<Button
														variant="outline"
														size="sm"
														onclick={addModel}
														disabled={!sel.model?.trim()}
														>
															Add
														</Button>
												</div>
											{/if}
										</div>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				{/if}

				<!-- Code block -->
				{#if tool.codeBlock}
					<div class="space-y-2">
						<Label class="text-caption-mono text-muted-foreground">
							Config snippet ({tool.codeBlock.language})
						</Label>
						<Textarea
							readonly
							value={replaceVars(tool.codeBlock.code)}
							rows={Math.min(16, tool.codeBlock.code.split('\n').length)}
							class="font-mono text-body-sm bg-background"
						/>
					</div>
				{/if}

				<!-- Model aliases -->
				{#if (tool.defaultModels?.length ?? 0) > 0}
					<div class="space-y-2">
						<Label class="text-caption-mono uppercase text-muted-foreground">Model aliases</Label>
						<div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
							{#each tool.defaultModels as dm}
								<button
									type="button"
									class="group flex flex-col gap-1 rounded-lg border border-border bg-background p-3 text-left transition-colors hover:border-primary/50 disabled:cursor-not-allowed disabled:opacity-50"
									onclick={() => openModelPicker(dm.alias)}
									disabled={models.length === 0}
								>
									<span class="text-caption-mono text-muted-foreground">{dm.name}</span>
									<span class="flex items-center justify-between gap-2">
										<span class="min-w-0 flex-1 truncate font-mono text-body-sm">
											{modelAliases[dm.alias] ?? dm.defaultValue ?? '— not set —'}
										</span>
										<SearchIcon
											class="size-3.5 shrink-0 text-muted-foreground group-hover:text-primary"
										/>
									</span>
								</button>
							{/each}
						</div>
					</div>
				{/if}

				<!-- Single model -->
				{#if (tool.guideSteps?.length ?? 0) === 0 && (tool.defaultModels?.length ?? 0) === 0}
					<div class="space-y-2">
						<Label class="text-caption-mono uppercase text-muted-foreground">Model</Label>
						<div class="flex gap-2">
							<Input
								bind:value={sel.model}
								placeholder="provider/model-id"
								class="font-mono text-body-sm flex-1"
							/>
							<Button
								variant="outline"
								size="sm"
								class="shrink-0 gap-1.5"
								onclick={() => openModelPicker('_main')}
								disabled={models.length === 0}
								><SearchIcon class="size-3.5" /> Browse</Button
							>
						</div>
					</div>
				{/if}

				<!-- Active model -->
				{#if showsActiveModel(tool)}
					<div class="space-y-2">
						<Label class="text-caption-mono uppercase text-muted-foreground">Active model</Label>
						<div class="flex gap-2">
							<Input
								bind:value={sel.activeModel}
								placeholder="provider/model-id (default when empty)"
								class="font-mono text-body-sm flex-1"
							/>
							<Button
								variant="outline"
								size="sm"
								class="shrink-0 gap-1.5"
								onclick={() => openModelPicker('activeModel')}
								disabled={models.length === 0}
								><SearchIcon class="size-3.5" /> Browse</Button
							>
						</div>
					</div>
				{/if}

				<!-- Subagent model -->
				{#if showsSubagentModel(tool)}
					<div class="space-y-2">
						<Label class="text-caption-mono uppercase text-muted-foreground">Subagent model</Label>
						<div class="flex gap-2">
							<Input
								bind:value={sel.subagentModel}
								placeholder="provider/model-id (defaults to main model)"
								class="font-mono text-body-sm flex-1"
							/>
							<Button
								variant="outline"
								size="sm"
								class="shrink-0 gap-1.5"
								onclick={() => openModelPicker('subagentModel')}
								disabled={models.length === 0}
								><SearchIcon class="size-3.5" /> Browse</Button
							>
						</div>
					</div>
				{/if}

				<!-- Base URL -->
				<div class="space-y-2">
					<Label class="text-caption-mono uppercase text-muted-foreground">Gateway Base URL</Label>
					<Input
						bind:value={sel.baseUrl}
						placeholder={defaultBaseUrl}
						class="font-mono text-body-sm"
					/>
				</div>

				<!-- Generated output (full-width below form on mobile, right column on lg) -->
				<div class="lg:hidden">
					<CLIConfigOutput {generated} {copiedField} onCopy={copyText} />
				</div>
			</div>

			<!-- Sticky side panel: actions + generated config on desktop -->
			<div class="flex flex-col gap-4">
				<div class="sticky top-4 flex flex-col gap-4 rounded-xl border border-border bg-card p-4 shadow-card">
					<!-- API Key -->
					<div class="space-y-2">
						<Label class="text-caption-mono uppercase text-muted-foreground">API Key</Label>
						<Select.Root
							type="single"
							value={sel.apiKeyId}
							onValueChange={(v: string) => (sel.apiKeyId = v)}
						>
							<Select.Trigger class="w-full h-10 text-body-sm">
								{@const selectedKey = keys.find((k) => k.id === sel.apiKeyId)}
								{selectedKey
									? `${selectedKey.name || 'Untitled'} (${selectedKey.key_preview})`
									: '— Select API key —'}
							</Select.Trigger>
							<Select.Content>
								<Select.Item value="">— Select API key —</Select.Item>
								{#each keys as key}
									<Select.Item value={key.id}>
										{key.name || 'Untitled'} ({key.key_preview})
									</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
						<p class="text-caption text-muted-foreground">
							The real key is pulled from this selection automatically — no need to paste it.
						</p>
					</div>

					<div class="flex flex-col gap-2">
						<Button variant="default" class="cursor-pointer" onclick={applyConfig} disabled={generating}>
							{generating ? 'Generating…' : 'Generate config'}
						</Button>
						<Button
							variant="outline"
							class="cursor-pointer gap-1.5"
							onclick={resetConfig}
							disabled={resetting || !detailConfigured}
						>
							<RotateCcwIcon class="size-3.5" />
							{resetting ? 'Resetting…' : 'Reset'}
						</Button>
					</div>

					<div class="hidden lg:block">
						<CLIConfigOutput {generated} {copiedField} onCopy={copyText} />
					</div>
				</div>
			</div>
		</div>
	{/if}

	<ModelPickerDialog
		bind:open={modelPickerOpen}
		{models}
		selectedModel={modelPickerTarget === '_main'
			? sel.model
			: modelPickerTarget === 'activeModel'
				? sel.activeModel ?? ''
				: modelPickerTarget === 'subagentModel'
					? sel.subagentModel ?? ''
					: (modelAliases[modelPickerTarget] || '')}
		selectedModels={sel.models}
		onSelect={onModelPick}
		onMultiSelect={onMultiSelect}
		multi={modelPickerMulti}
	/>
</div>
