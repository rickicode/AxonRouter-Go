<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { translatorApi, type TranslatorDetectResult } from '$lib/api';
	import { toast } from 'svelte-sonner';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import CheckIcon from '@lucide/svelte/icons/check';
	import BracesIcon from '@lucide/svelte/icons/braces';
	import SendIcon from '@lucide/svelte/icons/send';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
	import PlayIcon from '@lucide/svelte/icons/play';
	import EraserIcon from '@lucide/svelte/icons/eraser';

	// 7 steps matching requestLogger files exactly.
	const STEPS = [
		{ id: 1, label: 'Client Request', file: '1_req_client.json', lang: 'json', desc: 'Raw request from client' },
		{ id: 2, label: 'Source Body', file: '2_req_source.json', lang: 'json', desc: 'After initial conversion' },
		{ id: 3, label: 'OpenAI Intermediate', file: '3_req_openai.json', lang: 'json', desc: 'source → openai' },
		{ id: 4, label: 'Target Request', file: '4_req_target.json', lang: 'json', desc: 'openai → target + URL + headers' },
		{ id: 5, label: 'Provider Response', file: '5_res_provider.txt', lang: 'text', desc: 'Raw SSE from provider' },
		{ id: 6, label: 'OpenAI Response', file: '6_res_openai.txt', lang: 'text', desc: 'target → openai (response)' },
		{ id: 7, label: 'Client Response', file: '7_res_client.txt', lang: 'text', desc: 'Final response to client' },
	];

	let contents = $state<Record<number, string>>({});
	let expanded = $state<Record<number, boolean>>({ 1: true });
	let loading = $state<Record<string, boolean>>({});
	let meta = $state<TranslatorDetectResult | null>(null);
	let copiedId = $state<number | null>(null);

	function setContent(id: number, val: string) {
		contents[id] = val;
	}
	function setLoad(key: string, val: boolean) {
		loading[key] = val;
	}
	function toggle(id: number) {
		expanded[id] = !expanded[id];
	}
	function openNext(nextId: number) {
		const next: Record<number, boolean> = {};
		STEPS.forEach((s) => (next[s.id] = false));
		next[nextId] = true;
		expanded = next;
	}

	// Step 1: detect provider/model/formats from the client body.
	async function detectMeta(rawContent: string) {
		try {
			const body = JSON.parse(rawContent);
			const res = await translatorApi.detect(body);
			if (res.success && res.result && 'provider' in res.result) {
				meta = res.result as TranslatorDetectResult;
			}
		} catch {
			// Not valid JSON yet — ignore until the body is parseable.
		}
	}

	// Load a step file from logs/translator/.
	async function handleLoad(stepId: number) {
		const step = STEPS.find((s) => s.id === stepId)!;
		setLoad(`load-${stepId}`, true);
		try {
			const res = await translatorApi.load(step.file);
			if (res.success) {
				setContent(stepId, res.content);
				if (stepId === 1 && res.content.trim()) await detectMeta(res.content);
			} else {
				toast.error('Load failed: ' + (res as { error?: string }).error);
			}
		} catch (e) {
			toast.error('Load failed: ' + (e as Error).message);
		}
		setLoad(`load-${stepId}`, false);
	}

	// Step 1 → Step 3: source → OpenAI intermediate.
	async function handleToOpenAI() {
		setLoad('toOpenAI', true);
		try {
			const raw = contents[1];
			if (!raw || !raw.trim()) {
				toast.error('Step 1 is empty. Paste a client request first.');
				return;
			}
			const body = JSON.parse(raw);
			await translatorApi.save('1_req_client.json', raw);
			await translatorApi.save(
				'2_req_source.json',
				JSON.stringify({ timestamp: new Date().toISOString(), headers: {}, body: body.body || body }, null, 2),
			);

			const res = await translatorApi.toOpenAI(body);
			if (!res.success || !res.result || !('body' in res.result)) {
				toast.error(res.error || 'Step 2 failed');
				return;
			}
			const str = JSON.stringify(JSON.parse(res.result.body), null, 2);
			setContent(3, str);
			openNext(3);
			toast.success('Converted to OpenAI intermediate');
		} catch (e) {
			toast.error('Step 2 failed: ' + (e as Error).message);
		}
		setLoad('toOpenAI', false);
	}

	// Step 3 → Step 4: OpenAI → target + build URL/headers/body.
	async function handleToTarget() {
		setLoad('toTarget', true);
		try {
			const raw = contents[3];
			if (!raw || !raw.trim()) {
				toast.error('Step 3 is empty. Run → OpenAI first.');
				return;
			}
			const openaiBody = JSON.parse(raw);
			await translatorApi.save('3_req_openai.json', raw);

			const res = await translatorApi.toTarget(openaiBody, '', meta?.model || '');
			if (!res.success || !res.result || !('url' in res.result)) {
				toast.error(res.error || 'Step 3 failed');
				return;
			}
			// Embed provider + model so Send works even without meta.
			const step4 = {
				...res.result,
				provider: meta?.provider,
				model: meta?.model,
			};
			setContent(4, JSON.stringify(step4, null, 2));
			openNext(4);
			toast.success('Built target request');
		} catch (e) {
			toast.error('Step 3 failed: ' + (e as Error).message);
		}
		setLoad('toTarget', false);
	}

	// Step 4 → Step 5: send to the provider via the executor.
	async function handleSend() {
		setLoad('send', true);
		try {
			const raw = contents[4];
			if (!raw || !raw.trim()) {
				toast.error('Step 4 is empty. Run → Target first.');
				return;
			}
			const step4 = JSON.parse(raw);
			await translatorApi.save('4_req_target.json', raw);

			const provider = step4.provider || meta?.provider;
			const model = step4.model || meta?.model;
			if (!provider || !model) {
				toast.error('Missing provider or model. Please run step 1 first to detect them.');
				return;
			}

			let full = '';
			await translatorApi.send(provider, model, step4.body || step4, (text) => {
				full += text;
				// Live-update step 5 while streaming.
				setContent(5, full);
			});
			setContent(5, full);
			openNext(5);
			await translatorApi.save('5_res_provider.txt', full);
			toast.success('Provider response captured');
		} catch (e) {
			toast.error('Send failed: ' + (e as Error).message);
		}
		setLoad('send', false);
	}

	async function handleCopy(id: number) {
		if (!contents[id]) return;
		try {
			await navigator.clipboard.writeText(contents[id]);
			copiedId = id;
			setTimeout(() => (copiedId = null), 1500);
		} catch (e) {
			toast.error('Copy failed: ' + (e as Error).message);
		}
	}

	function handleFormat(id: number) {
		try {
			const obj = JSON.parse(contents[id]);
			setContent(id, JSON.stringify(obj, null, 2));
			toast.success(`Step ${id} formatted`);
		} catch {
			// Not JSON — leave as-is.
		}
	}

	function handleClear(id: number) {
		setContent(id, '');
		toast.info(`Step ${id} cleared`);
	}

	// Action kind per step (matches 9router): null for steps with no action.
	function actionKind(stepId: number): 'toOpenAI' | 'toTarget' | 'send' | null {
		if (stepId === 1) return 'toOpenAI';
		if (stepId === 3) return 'toTarget';
		if (stepId === 4) return 'send';
		return null;
	}

	onMount(() => {
		// Auto-load the saved session if present.
		handleLoad(1);
	});
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<!-- Header -->
	<div class="flex items-start justify-between gap-4 flex-wrap">
		<div class="space-y-1">
			<h1 class="text-display-lg">Translator Debug.</h1>
			<p class="text-body-sm text-muted-foreground">
				Replay the request pipeline — client → source → OpenAI → target → provider → client. Matches the log files under
				logs/translator/.
			</p>
		</div>
		{#if meta}
			<div class="flex items-center gap-2 flex-wrap justify-end">
				<Badge variant="secondary" class="font-mono text-caption">
					src: {meta.sourceFormat}
				</Badge>
				<ArrowRightIcon class="size-3.5 text-muted-foreground" />
				<Badge variant="secondary" class="font-mono text-caption">
					dst: {meta.targetFormat}
				</Badge>
				<Badge variant="secondary" class="font-mono text-caption">
					{meta.provider}
				</Badge>
				<Badge variant="secondary" class="font-mono text-caption">
					{meta.model}
				</Badge>
			</div>
		{/if}
	</div>

	{#each STEPS as step}
		{@const isExpanded = !!expanded[step.id]}
		{@const content = contents[step.id] || ''}
		{@const action = actionKind(step.id)}
		{@const actionLabel = action === 'toOpenAI' ? 'OpenAI' : action === 'toTarget' ? 'Target' : action === 'send' ? 'Send' : ''}
		{@const actionBusy =
			action === 'toOpenAI' ? loading['toOpenAI'] : action === 'toTarget' ? loading['toTarget'] : action === 'send' ? loading['send'] : false}
		{@const actionClick =
			action === 'toOpenAI' ? handleToOpenAI : action === 'toTarget' ? handleToTarget : action === 'send' ? handleSend : null}
		<Card class="bg-card border-border rounded-xl shadow-card">
			<CardContent class="p-4 space-y-3">
				<!-- Step header -->
				<div class="flex items-center justify-between gap-2">
					<button
						class="flex items-center gap-2 flex-1 text-left group cursor-pointer"
						onclick={() => toggle(step.id)}
					>
						{#if isExpanded}
							<ChevronDownIcon class="size-4 text-muted-foreground group-hover:text-foreground transition-colors shrink-0" />
						{:else}
							<ChevronRightIcon class="size-4 text-muted-foreground group-hover:text-foreground transition-colors shrink-0" />
						{/if}
						<span class="text-caption-mono text-muted-foreground/60 w-4">{step.id}</span>
						<h3 class="text-body-sm-strong text-foreground">{step.label}</h3>
						<span class="text-caption-mono text-muted-foreground/60">{step.file}</span>
						{#if content}
							<span class="text-caption text-emerald-500">({content.length} chars)</span>
						{/if}
					</button>
					{#if !isExpanded}
						<div class="flex gap-1 shrink-0">
							<Button
								variant="ghost"
								size="sm"
								class="cursor-pointer"
								disabled={loading[`load-${step.id}`]}
								onclick={() => handleLoad(step.id)}
								aria-label={`Load ${step.file}`}
							>
								{#if loading[`load-${step.id}`]}
									...
								{:else}
									<FolderOpenIcon class="size-4" />
								{/if}
							</Button>
							{#if action && actionClick}
								<Button variant="default" size="sm" class="gap-1 cursor-pointer" disabled={actionBusy} onclick={actionClick}>
									{#if actionBusy}
										...
									{:else if action === 'send'}
										<SendIcon class="size-3.5" />
									{:else}
										<ArrowRightIcon class="size-3.5" />
									{/if}
									{actionLabel}
								</Button>
							{/if}
						</div>
					{/if}
				</div>

				{#if isExpanded}
					<p class="text-caption text-muted-foreground">{step.desc}</p>
					<textarea
						class="w-full h-72 resize-y rounded-lg border border-border bg-background p-3 font-mono text-caption text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
						spellcheck="false"
						placeholder={`Paste ${step.label} here...`}
						value={content}
						oninput={(e) => {
							setContent(step.id, e.currentTarget.value);
							if (step.id === 1) detectMeta(e.currentTarget.value);
						}}
					/>
					<div class="flex gap-2 flex-wrap items-center">
						<Button
							variant="outline"
							size="sm"
							class="gap-1.5 rounded-sm cursor-pointer"
							disabled={loading[`load-${step.id}`]}
							onclick={() => handleLoad(step.id)}
						>
							{#if loading[`load-${step.id}`]}
								...
							{:else}
								<FolderOpenIcon class="size-3.5" />
							{/if}
							Load
						</Button>
						<Button variant="outline" size="sm" class="gap-1.5 rounded-sm cursor-pointer" onclick={() => handleFormat(step.id)}>
							<BracesIcon class="size-3.5" />
							Format
						</Button>
						<Button variant="outline" size="sm" class="gap-1.5 rounded-sm cursor-pointer" onclick={() => handleCopy(step.id)}>
							{#if copiedId === step.id}
								<CheckIcon class="size-3.5" />
							{:else}
								<CopyIcon class="size-3.5" />
							{/if}
							Copy
						</Button>
						<Button variant="ghost" size="sm" class="gap-1.5 rounded-sm cursor-pointer" onclick={() => handleClear(step.id)}>
							<EraserIcon class="size-3.5" />
							Clear
						</Button>
						<div class="ml-auto flex gap-2">
							{#if action && actionClick}
								<Button variant="default" size="sm" class="gap-1.5 rounded-sm cursor-pointer" disabled={actionBusy} onclick={actionClick}>
									{#if actionBusy}
										...
									{:else if action === 'send'}
										<SendIcon class="size-3.5" />
									{:else}
										<ArrowRightIcon class="size-3.5" />
									{/if}
									{actionLabel}
								</Button>
							{/if}
							{#if step.id === 4 && meta}
								<Button variant="outline" size="sm" class="gap-1.5 rounded-sm cursor-pointer" onclick={handleSend} disabled={loading['send']}>
									<PlayIcon class="size-3.5" />
									Re-send
								</Button>
							{/if}
						</div>
					</div>
				{/if}
			</CardContent>
		</Card>
	{/each}
</div>
