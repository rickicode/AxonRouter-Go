<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { usageApi, apiKeysApi, providersApi, type UsageData, type UsageBreakdown, type UsageTimeBucket, type APIKeyItem, type Provider } from '$lib/api';
	import { formatTokens, formatCost } from '$lib/stores';
	import { toast } from 'svelte-sonner';

	import BarChartIcon from '@lucide/svelte/icons/bar-chart';
	import CalendarIcon from '@lucide/svelte/icons/calendar';
	import KeyIcon from '@lucide/svelte/icons/key';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import ServerIcon from '@lucide/svelte/icons/server';
	import DollarSignIcon from '@lucide/svelte/icons/dollar-sign';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import TimerIcon from '@lucide/svelte/icons/timer';
	import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import CodeIcon from '@lucide/svelte/icons/code';

	let data = $state<UsageData | null>(null);
	let loading = $state(false);
	let apiKeys = $state<APIKeyItem[]>([]);
	let providers = $state<Provider[]>([]);

	let from = $state('');
	let to = $state('');
	let granularity = $state<'day' | 'month'>('day');
	let filterKey = $state('');
	let filterProvider = $state('');
	let filterModel = $state('');
	let filterModality = $state('');
	let filterStatus = $state('');

	let tokensCanvas: HTMLCanvasElement | null = null;
	let costCanvas: HTMLCanvasElement | null = null;
	let tokensChart = $state<any>(null);
	let costChart = $state<any>(null);

	function today(): string {
		return new Date().toISOString().split('T')[0];
	}

	function daysAgo(n: number): string {
		const d = new Date();
		d.setDate(d.getDate() - n);
		return d.toISOString().split('T')[0];
	}

	function startOfMonth(): string {
		const d = new Date();
		return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`;
	}

	function fmtPercent(n: number): string {
		return `${(n * 100).toFixed(1)}%`;
	}

	function fmtLatency(ms: number): string {
		if (ms < 1000) return `${Math.round(ms)}ms`;
		return `${(ms / 1000).toFixed(2)}s`;
	}

	function fmtDateTime(ts?: number): string {
		if (!ts) return '—';
		return new Date(ts * 1000).toLocaleString('en-GB', { hour12: false });
	}

	function fmtDate(ts?: string): string {
		if (!ts) return '—';
		return new Date(ts + 'T00:00:00').toLocaleDateString('en-GB', { day: '2-digit', month: 'short' });
	}

	async function load() {
		if (!from || !to) return;
		loading = true;
		try {
			const statusCode = filterStatus ? parseInt(filterStatus, 10) : undefined;
			const res = await usageApi.get({
				from,
				to,
				granularity,
				api_key_id: filterKey || undefined,
				provider_id: filterProvider || undefined,
				model_id: filterModel || undefined,
				modality: filterModality || undefined,
				status_code: statusCode,
			});
			data = res.data;
			await tick();
			updateCharts();
		} catch (err) {
			toast.error('Failed to load usage: ' + (err instanceof Error ? err.message : 'unknown error'));
		} finally {
			loading = false;
		}
	}

	function setRange(start: string, end: string, g: 'day' | 'month' = 'day') {
		from = start;
		to = end;
		granularity = g;
		void load();
	}

	function resetFilters() {
		filterKey = '';
		filterProvider = '';
		filterModel = '';
		filterModality = '';
		filterStatus = '';
		void load();
	}

	async function initPage() {
		await Promise.all([
			apiKeysApi.list().then((r) => { apiKeys = r.data; }).catch(() => {}),
			providersApi.list().then((r) => { providers = r.data; }).catch(() => {}),
		]);
		setRange(daysAgo(30), today(), 'day');
	}

	onMount(() => {
		document.title = 'Usage — AxonRouter';
		void initPage();
		return () => {
			tokensChart?.destroy();
			costChart?.destroy();
		};
	});

	function chartOptions(title: string): any {
		return {
			responsive: true,
			maintainAspectRatio: false,
			plugins: {
				legend: { labels: { color: '#a1a1aa', font: { size: 11 } } },
				title: { display: false },
				tooltip: {
					backgroundColor: '#18181b',
					titleColor: '#fafafa',
					bodyColor: '#d4d4d8',
					borderColor: '#27272a',
					borderWidth: 1,
				},
			},
			scales: {
				x: {
					grid: { color: '#27272a' },
					ticks: { color: '#a1a1aa', font: { size: 10 }, maxRotation: 0, autoSkip: true },
				},
				y: {
					grid: { color: '#27272a' },
					ticks: { color: '#a1a1aa', font: { size: 10 } },
				},
			},
		};
	}

	async function updateCharts() {
		if (!data) return;

		if (tokensCanvas && data?.by_time.length) {
		if (tokensChart) tokensChart.destroy();
			const { default: Chart } = await import('chart.js/auto');
			tokensChart = new Chart(tokensCanvas, {
				type: 'bar',
				data: {
					labels: data.by_time.map((r) => r.bucket),
					datasets: [
						{ label: 'Input', data: data.by_time.map((r) => r.input_tokens), backgroundColor: '#22d3ee' },
						{ label: 'Output', data: data.by_time.map((r) => r.output_tokens), backgroundColor: '#a78bfa' },
						{ label: 'Reasoning', data: data.by_time.map((r) => r.reasoning_tokens), backgroundColor: '#f472b6' },
					],
				},
				options: {
					...chartOptions('Tokens'),
					scales: {
						...chartOptions('Tokens').scales,
						x: { stacked: true, grid: { color: '#27272a' }, ticks: { color: '#a1a1aa', font: { size: 10 }, maxRotation: 0, autoSkip: true } },
						y: { stacked: true, grid: { color: '#27272a' }, ticks: { color: '#a1a1aa', font: { size: 10 } } },
					},
				},
			});
		}

		if (costCanvas && data.by_time.length) {
			if (costChart) costChart.destroy();
			const { default: Chart } = await import('chart.js/auto');
			costChart = new Chart(costCanvas, {
				type: 'line',
				data: {
					labels: data.by_time.map((r) => r.bucket),
					datasets: [
						{
							label: 'Cost (USD)',
							data: data.by_time.map((r) => r.cost_usd),
							borderColor: '#34d399',
							backgroundColor: 'rgba(52, 211, 153, 0.15)',
							fill: true,
							tension: 0.35,
							pointRadius: 3,
						},
					],
				},
				options: chartOptions('Cost'),
			});
		}
	}

	function exportJSON() {
		if (!data) return;
		const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `usage-${from}_to_${to}.json`;
		a.click();
		URL.revokeObjectURL(url);
		toast.success('Exported JSON');
	}

	function exportCSV(rows: UsageBreakdown[], name: string) {
		if (!rows.length) return;
		const headers = Object.keys(rows[0]);
		const csv = [
			headers.join(','),
			...rows.map((r) => headers.map((h) => JSON.stringify((r as any)[h])).join(',')),
		].join('\n');
		const blob = new Blob([csv], { type: 'text/csv' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `usage-${name}-${from}_to_${to}.csv`;
		a.click();
		URL.revokeObjectURL(url);
		toast.success(`Exported ${name} CSV`);
	}

	function shareOf(value: number, total: number): string {
		if (!total) return '0%';
		return `${((value / total) * 100).toFixed(1)}%`;
	}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="space-y-1">
		<h1 class="text-display-lg">Usage.</h1>
		<p class="text-body-sm text-muted-foreground">Deep analytics: tokens, cost, latency, errors per API key, model, provider, modality, status, and time.</p>
	</div>

	<Card class="shadow-card">
		<CardContent class="p-4">
			<div class="flex flex-wrap items-end gap-3">
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">From</Label>
					<Input type="date" bind:value={from} class="h-9 text-sm w-40" />
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">To</Label>
					<Input type="date" bind:value={to} class="h-9 text-sm w-40" />
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Granularity</Label>
					<div class="flex gap-1">
						<Button variant={granularity === 'day' ? 'default' : 'outline'} size="sm" class="text-body-sm cursor-pointer" onclick={() => { granularity = 'day'; void load(); }}>Day</Button>
						<Button variant={granularity === 'month' ? 'default' : 'outline'} size="sm" class="text-body-sm cursor-pointer" onclick={() => { granularity = 'month'; void load(); }}>Month</Button>
					</div>
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Quick</Label>
					<div class="flex gap-1">
						<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => setRange(daysAgo(7), today())}>7d</Button>
						<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => setRange(daysAgo(30), today())}>30d</Button>
						<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => setRange(startOfMonth(), today(), 'month')}>Month</Button>
					</div>
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Actions</Label>
					<div class="flex gap-1">
						<Button size="sm" class="text-body-sm cursor-pointer" onclick={() => void load()} disabled={loading}>
							<RefreshCwIcon class="w-4 h-4 mr-1" />
							{loading ? 'Loading...' : 'Refresh'}
						</Button>
					</div>
				</div>
			</div>

			<div class="mt-4 pt-4 border-t border-border flex flex-wrap items-end gap-3">
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">API Key</Label>
					<select bind:value={filterKey} class="h-9 text-sm rounded-sm border border-input bg-transparent px-3 py-1 w-48 text-foreground">
						<option value="">All keys</option>
						{#each apiKeys as k}
							<option value={k.id}>{k.name || k.id}</option>
						{/each}
					</select>
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Provider</Label>
					<select bind:value={filterProvider} class="h-9 text-sm rounded-sm border border-input bg-transparent px-3 py-1 w-48 text-foreground">
						<option value="">All providers</option>
						{#each providers as p}
							<option value={p.id}>{p.display_name || p.id}</option>
						{/each}
					</select>
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Model</Label>
					<Input bind:value={filterModel} placeholder="e.g. cx/gpt-5.4" class="h-9 text-sm w-48" />
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Modality</Label>
					<select bind:value={filterModality} class="h-9 text-sm rounded-sm border border-input bg-transparent px-3 py-1 w-40 text-foreground">
						<option value="">All</option>
						<option value="chat">chat</option>
						<option value="messages">messages</option>
						<option value="responses">responses</option>
						<option value="embeddings">embeddings</option>
						<option value="images">images</option>
						<option value="video">video</option>
						<option value="tts">tts</option>
						<option value="stt">stt</option>
					</select>
				</div>
				<div class="flex flex-col gap-1.5">
					<Label class="text-caption-mono text-muted-foreground">Status</Label>
					<Input bind:value={filterStatus} type="number" placeholder="e.g. 200" class="h-9 text-sm w-28" />
				</div>
				<div class="flex gap-2 ml-auto">
					<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => void load()}>
						<FilterIcon class="w-4 h-4 mr-1" />
						Apply
					</Button>
					<Button variant="ghost" size="sm" class="text-body-sm cursor-pointer" onclick={resetFilters}>Reset</Button>
				</div>
			</div>
		</CardContent>
	</Card>

	{#if data}
		<div class="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-8 gap-3">
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Requests</p>
					<p class="text-display-md">{data.summary.requests.toLocaleString()}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Total Tokens</p>
					<p class="text-display-md">{formatTokens(data.summary.total_tokens)}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Input</p>
					<p class="text-display-md">{formatTokens(data.summary.input_tokens)}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Output</p>
					<p class="text-display-md">{formatTokens(data.summary.output_tokens)}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Reasoning</p>
					<p class="text-display-md">{formatTokens(data.summary.reasoning_tokens)}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Cost</p>
					<p class="text-display-md">{formatCost(data.summary.cost_usd)}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Avg Latency</p>
					<p class="text-display-md">{fmtLatency(data.summary.avg_latency_ms)}</p>
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-3">
					<p class="text-caption text-muted-foreground uppercase">Errors</p>
					<p class="text-display-md flex items-center gap-1.5">
						{data.summary.errors.toLocaleString()}
						<Badge variant={data.summary.error_rate > 0.05 ? 'destructive' : 'secondary'} class="text-caption-mono rounded-sm">{fmtPercent(data.summary.error_rate)}</Badge>
					</p>
				</CardContent>
			</Card>
		</div>

		{#if data.by_time.length}
			<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<Card class="shadow-card">
					<CardHeader class="pb-2 flex flex-row items-center justify-between">
						<CardTitle class="text-body-sm-strong flex items-center gap-2">
							<BarChartIcon class="w-4 h-4" />
							Tokens over time
						</CardTitle>
					</CardHeader>
					<CardContent class="p-4 h-64">
						<canvas bind:this={tokensCanvas}></canvas>
					</CardContent>
				</Card>
				<Card class="shadow-card">
					<CardHeader class="pb-2 flex flex-row items-center justify-between">
						<CardTitle class="text-body-sm-strong flex items-center gap-2">
							<DollarSignIcon class="w-4 h-4" />
							Cost over time
						</CardTitle>
					</CardHeader>
					<CardContent class="p-4 h-64">
						<canvas bind:this={costCanvas}></canvas>
					</CardContent>
				</Card>
			</div>
		{/if}

		<div class="flex justify-end gap-2">
			<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={exportJSON}>
				<DownloadIcon class="w-4 h-4 mr-1" />
				Export JSON
			</Button>
			<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => exportCSV(data!.by_api_key, 'api-keys')}>
				<DownloadIcon class="w-4 h-4 mr-1" />
				Export API Keys CSV
			</Button>
		</div>

		<div class="grid grid-cols-1 xl:grid-cols-2 gap-6">
			{@render breakdownTable('By API Key', KeyIcon, data.by_api_key, (r) => r.api_key_name || r.api_key_id || 'unauthenticated', (r) => r.api_key_id, true)}
			{@render breakdownTable('By Model', CpuIcon, data.by_model, (r) => r.model_id || 'unknown', undefined, false)}
		</div>
		<div class="grid grid-cols-1 xl:grid-cols-2 gap-6">
			{@render breakdownTable('By Provider', ServerIcon, data.by_provider, (r) => r.provider_name || r.provider_id || 'unknown', (r) => r.provider_id, false)}
			{@render breakdownTable('By Modality', LayersIcon, data.by_modality, (r) => r.modality || 'unknown', undefined, false)}
		</div>

		<Card class="shadow-card overflow-hidden">
			<CardHeader class="pb-2 flex flex-row items-center justify-between">
				<CardTitle class="text-body-sm-strong flex items-center gap-2">
					<ActivityIcon class="w-4 h-4" />
					By Status Code
				</CardTitle>
			</CardHeader>
			<CardContent class="p-0 overflow-x-auto">
				{#if data.by_status.length}
					<table class="w-full text-left border-collapse min-w-[600px]">
						<thead>
							<tr class="border-b border-border bg-muted/30">
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3">Status</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Requests</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Tokens</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Cost</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Errors</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Avg Latency</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">First</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Last</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-border">
							{#each data.by_status as row}
								<tr class="transition-colors hover:bg-accent/20">
									<td class="py-2 px-3">
										<Badge variant={row.status_code >= 400 ? 'destructive' : 'default'} class="text-caption-mono rounded-sm font-mono">{row.status_code || 0}</Badge>
									</td>
									<td class="py-2 px-3 text-caption-mono text-right">{row.requests.toLocaleString()}</td>
									<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.total_tokens)}</td>
									<td class="py-2 px-3 text-caption-mono text-right">{formatCost(row.cost_usd)}</td>
									<td class="py-2 px-3 text-caption-mono text-right">{row.errors.toLocaleString()}</td>
									<td class="py-2 px-3 text-caption-mono text-right">{fmtLatency(row.avg_latency_ms)}</td>
									<td class="py-2 px-3 text-caption-mono text-right">{fmtDateTime(row.first_request_at)}</td>
									<td class="py-2 px-3 text-caption-mono text-right">{fmtDateTime(row.last_request_at)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{:else}
					<p class="p-4 text-body-sm text-muted-foreground">No status data.</p>
				{/if}
			</CardContent>
		</Card>
	{:else if !loading}
		<Card class="shadow-card">
			<CardContent class="p-6 text-body-sm text-muted-foreground">Select a date range to view usage analytics.</CardContent>
		</Card>
	{/if}
</div>

{#snippet breakdownTable(
	title: string,
	Icon: any,
	rows: UsageBreakdown[],
	label: (r: UsageBreakdown) => string,
	subLabel?: (r: UsageBreakdown) => string | undefined,
	showExport: boolean
)}
<Card class="shadow-card overflow-hidden">
	<CardHeader class="pb-2 flex flex-row items-center justify-between">
		<CardTitle class="text-body-sm-strong flex items-center gap-2">
			<Icon class="w-4 h-4" />
			{title}
		</CardTitle>
		{#if showExport}
			<Button variant="ghost" size="sm" class="h-7 px-2 text-body-sm cursor-pointer" onclick={() => exportCSV(rows, title.toLowerCase().replace(/\s+/g, '-'))}>
				<DownloadIcon class="w-3.5 h-3.5" />
			</Button>
		{/if}
	</CardHeader>
	<CardContent class="p-0 overflow-x-auto">
		{#if rows.length}
			<table class="w-full text-left border-collapse min-w-[700px]">
				<thead>
					<tr class="border-b border-border bg-muted/30">
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3">Name</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Req</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Input</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Output</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Total</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Cost</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Err%</th>
						<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Avg</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-border">
					{#each rows as row}
						<tr class="transition-colors hover:bg-accent/20">
							<td class="py-2 px-3 text-body-sm">
								<div class="font-medium truncate max-w-[180px]" title={label(row)}>{label(row)}</div>
								{#if subLabel}
									<div class="font-mono text-xs text-muted-foreground truncate max-w-[180px]" title={subLabel(row)}>{subLabel(row)}</div>
								{/if}
							</td>
							<td class="py-2 px-3 text-caption-mono text-right">{row.requests.toLocaleString()}</td>
							<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.input_tokens)}</td>
							<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.output_tokens)}</td>
							<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.total_tokens)}</td>
							<td class="py-2 px-3 text-caption-mono text-right">{formatCost(row.cost_usd)}</td>
							<td class="py-2 px-3 text-right">
								<Badge variant={row.error_rate > 0.05 ? 'destructive' : 'secondary'} class="text-caption-mono rounded-sm">{fmtPercent(row.error_rate)}</Badge>
							</td>
							<td class="py-2 px-3 text-caption-mono text-right">{fmtLatency(row.avg_latency_ms)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{:else}
			<p class="p-4 text-body-sm text-muted-foreground">No data for this breakdown.</p>
		{/if}
	</CardContent>
</Card>
{/snippet}
