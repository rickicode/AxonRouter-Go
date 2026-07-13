<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import { usageApi, type UsageData } from '$lib/api';
	import { formatTokens, formatCost } from '$lib/stores';
	import { toast } from 'svelte-sonner';
	import BarChartIcon from '@lucide/svelte/icons/bar-chart';
	import CalendarIcon from '@lucide/svelte/icons/calendar';
	import KeyIcon from '@lucide/svelte/icons/key';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import ServerIcon from '@lucide/svelte/icons/server';
	import DollarSignIcon from '@lucide/svelte/icons/dollar-sign';
	import ActivityIcon from '@lucide/svelte/icons/activity';

	let data = $state<UsageData | null>(null);
	let loading = $state(false);
	let from = $state('');
	let to = $state('');
	let granularity = $state<'day' | 'month'>('day');

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

	async function load() {
		if (!from || !to) return;
		loading = true;
		try {
			const res = await usageApi.get({ from, to, granularity });
			data = res.data;
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

	onMount(() => {
		document.title = 'Usage — AxonRouter';
		setRange(daysAgo(30), today(), 'day');
	});

	function maxTimeValue(rows: { tokens: number }[]): number {
		if (!rows.length) return 0;
		return Math.max(...rows.map((r) => r.tokens));
	}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="space-y-1">
		<h1 class="text-display-lg">Usage.</h1>
		<p class="text-body-sm text-muted-foreground">Track tokens, cost, and requests by API key, model, provider, and time range.</p>
	</div>

	<Card class="shadow-card">
		<CardContent class="p-4">
			<div class="flex flex-wrap items-center gap-3">
				<div class="flex items-center gap-2">
					<CalendarIcon class="w-4 h-4 text-muted-foreground" />
					<Input type="date" bind:value={from} class="h-9 text-sm w-40" />
					<span class="text-muted-foreground">–</span>
					<Input type="date" bind:value={to} class="h-9 text-sm w-40" />
				</div>
				<div class="flex items-center gap-2">
					<Button variant={granularity === 'day' ? 'default' : 'outline'} size="sm" class="text-body-sm cursor-pointer" onclick={() => { granularity = 'day'; void load(); }}>Day</Button>
					<Button variant={granularity === 'month' ? 'default' : 'outline'} size="sm" class="text-body-sm cursor-pointer" onclick={() => { granularity = 'month'; void load(); }}>Month</Button>
				</div>
				<div class="flex items-center gap-2">
					<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => setRange(daysAgo(7), today())}>7d</Button>
					<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => setRange(daysAgo(30), today())}>30d</Button>
					<Button variant="outline" size="sm" class="text-body-sm cursor-pointer" onclick={() => setRange(startOfMonth(), today(), 'month')}>This month</Button>
				</div>
				<Button size="sm" class="text-body-sm cursor-pointer" onclick={() => void load()} disabled={loading}>
					{loading ? 'Loading...' : 'Refresh'}
				</Button>
			</div>
		</CardContent>
	</Card>

	{#if data}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
			<Card class="shadow-card">
				<CardContent class="p-4 flex items-center justify-between">
					<div>
						<p class="text-caption text-muted-foreground uppercase">Requests</p>
						<p class="text-display-md">{data.summary.requests.toLocaleString()}</p>
					</div>
					<ActivityIcon class="w-6 h-6 text-muted-foreground" />
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-4 flex items-center justify-between">
					<div>
						<p class="text-caption text-muted-foreground uppercase">Total Tokens</p>
						<p class="text-display-md">{formatTokens(data.summary.total_tokens)}</p>
					</div>
					<CpuIcon class="w-6 h-6 text-muted-foreground" />
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-4 flex items-center justify-between">
					<div>
						<p class="text-caption text-muted-foreground uppercase">Input / Output</p>
						<p class="text-display-md">{formatTokens(data.summary.input_tokens)} / {formatTokens(data.summary.output_tokens)}</p>
					</div>
					<BarChartIcon class="w-6 h-6 text-muted-foreground" />
				</CardContent>
			</Card>
			<Card class="shadow-card">
				<CardContent class="p-4 flex items-center justify-between">
					<div>
						<p class="text-caption text-muted-foreground uppercase">Cost</p>
						<p class="text-display-md">{formatCost(data.summary.cost_usd)}</p>
					</div>
					<DollarSignIcon class="w-6 h-6 text-muted-foreground" />
				</CardContent>
			</Card>
		</div>

		{#if data.by_time.length}
			<Card class="shadow-card">
				<CardHeader class="pb-2">
					<CardTitle class="text-body-sm-strong">Tokens over time</CardTitle>
				</CardHeader>
				<CardContent class="p-4">
					{@const max = maxTimeValue(data.by_time)}
					<div class="flex items-end gap-2 h-40">
						{#each data.by_time as row}
							<div class="flex-1 flex flex-col justify-end group relative">
								<div class="bg-primary/80 rounded-sm w-full transition-all group-hover:bg-primary" style="height: {max ? (row.tokens / max) * 100 : 0}%"></div>
								<p class="text-caption-mono text-center mt-2 text-muted-foreground truncate">{row.bucket}</p>
								<div class="absolute bottom-14 left-1/2 -translate-x-1/2 px-2 py-1 rounded-sm bg-popover text-popover-foreground text-xs opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none whitespace-nowrap border border-border">
									{formatTokens(row.tokens)} · {formatCost(row.cost_usd)}
								</div>
							</div>
						{/each}
					</div>
				</CardContent>
			</Card>
		{/if}

		<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
			<Card class="shadow-card overflow-hidden">
				<CardHeader class="pb-2">
					<CardTitle class="text-body-sm-strong flex items-center gap-2">
						<KeyIcon class="w-4 h-4" />
						By API Key
					</CardTitle>
				</CardHeader>
				<CardContent class="p-0">
					{#if data.by_api_key.length}
						<table class="w-full text-left border-collapse">
							<thead>
								<tr class="border-b border-border bg-muted/30">
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3">Key</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Req</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Tokens</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Cost</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#each data.by_api_key as row}
									<tr class="transition-colors hover:bg-accent/20">
										<td class="py-2 px-3 text-body-sm">
											<div class="font-medium">{row.api_key_name || '—'}</div>
											<div class="font-mono text-xs text-muted-foreground break-all">{row.api_key_id || 'unauthenticated'}</div>
										</td>
										<td class="py-2 px-3 text-caption-mono text-right">{row.requests.toLocaleString()}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.total_tokens)}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{formatCost(row.cost_usd)}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{:else}
						<p class="p-4 text-body-sm text-muted-foreground">No usage data for this range.</p>
					{/if}
				</CardContent>
			</Card>

			<Card class="shadow-card overflow-hidden">
				<CardHeader class="pb-2">
					<CardTitle class="text-body-sm-strong flex items-center gap-2">
						<CpuIcon class="w-4 h-4" />
						By Model
					</CardTitle>
				</CardHeader>
				<CardContent class="p-0">
					{#if data.by_model.length}
						<table class="w-full text-left border-collapse">
							<thead>
								<tr class="border-b border-border bg-muted/30">
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3">Model</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Req</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Tokens</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Cost</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#each data.by_model as row}
									<tr class="transition-colors hover:bg-accent/20">
										<td class="py-2 px-3 text-body-sm font-mono text-xs break-all">{row.model_id}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{row.requests.toLocaleString()}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.total_tokens)}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{formatCost(row.cost_usd)}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{:else}
						<p class="p-4 text-body-sm text-muted-foreground">No usage data for this range.</p>
					{/if}
				</CardContent>
			</Card>

			<Card class="shadow-card overflow-hidden">
				<CardHeader class="pb-2">
					<CardTitle class="text-body-sm-strong flex items-center gap-2">
						<ServerIcon class="w-4 h-4" />
						By Provider
					</CardTitle>
				</CardHeader>
				<CardContent class="p-0">
					{#if data.by_provider.length}
						<table class="w-full text-left border-collapse">
							<thead>
								<tr class="border-b border-border bg-muted/30">
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3">Provider</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Req</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Tokens</th>
									<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-3 text-right">Cost</th>
								</tr>
							</thead>
							<tbody class="divide-y divide-border">
								{#each data.by_provider as row}
									<tr class="transition-colors hover:bg-accent/20">
										<td class="py-2 px-3 text-body-sm">
											<div class="font-medium">{row.provider_name || '—'}</div>
											<div class="font-mono text-xs text-muted-foreground">{row.provider_id || 'unknown'}</div>
										</td>
										<td class="py-2 px-3 text-caption-mono text-right">{row.requests.toLocaleString()}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{formatTokens(row.total_tokens)}</td>
										<td class="py-2 px-3 text-caption-mono text-right">{formatCost(row.cost_usd)}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{:else}
						<p class="p-4 text-body-sm text-muted-foreground">No usage data for this range.</p>
					{/if}
				</CardContent>
			</Card>
		</div>
	{:else if !loading}
		<Card class="shadow-card">
			<CardContent class="p-6 text-body-sm text-muted-foreground">Select a date range to view usage.</CardContent>
		</Card>
	{/if}
</div>
