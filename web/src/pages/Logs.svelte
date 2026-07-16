<script lang="ts">
	import { onMount } from 'svelte';
	import {
		loadLogs,
		logs,
		logPagination,
		logFilter,
		activeRequests,
		loadActiveRequests,
		refreshLogs,
		isLoading,
		error,
		formatLatency,
		formatTokens,
		formatCost,
} from '$lib/stores';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import ActiveOctopus from '$lib/components/ActiveOctopus.svelte';
	import ProviderIcon from '$lib/components/ProviderIcon.svelte';
	import { getProviderMeta } from '$lib/provider-catalog';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import * as Dialog from '$lib/components/ui/dialog';
	import Pagination from '$lib/components/Pagination.svelte';
	import { type RequestLog } from '$lib/api';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import ActivityIcon from '@lucide/svelte/icons/activity';

	let currentPage = $state(1);
	let perPage = $state(100);
	let selectedErrorLog = $state<RequestLog | null>(null);

	onMount(() => {
		document.title = 'Logs — AxonRouter';
		loadLogs(currentPage, perPage);
		loadActiveRequests();
		const activeInterval = setInterval(loadActiveRequests, 3000);
		const logsInterval = setInterval(() => refreshLogs(currentPage, perPage), 3000);
		return () => {
			clearInterval(activeInterval);
			clearInterval(logsInterval);
		};
	});

	function handlePageChange(page: number) {
		currentPage = page;
		loadLogs(currentPage, perPage);
	}

	function handlePerPageChange(p: number) {
		perPage = p;
		currentPage = 1;
		loadLogs(currentPage, perPage);
	}

	function handleFilterChange() {
		currentPage = 1;
		loadLogs(currentPage, perPage);
	}

	function setStatusFilter(code: number) {
		$logFilter.status_code = code;
		handleFilterChange();
	}

	function clearFilters() {
		$logFilter = {
			provider_id: '',
			connection_id: '',
			model_id: '',
			status_code: 0,
			start_date: '',
			end_date: '',
		};
		handleFilterChange();
	}

	function formatDurationMs(startedAt: number, now = Date.now()): string {
		const ms = now - startedAt;
		if (ms < 1000) return `${ms}ms`;
		return `${(ms / 1000).toFixed(1)}s`;
	}

	function formatLogTime(ts: number): string {
		// request_logs stores timestamps as Unix milliseconds.
		const d = ts > 1_000_000_000_000 ? new Date(ts) : new Date(ts * 1000);
		return isNaN(d.getTime()) ? '—' : d.toLocaleString();
	}

	function handleExport() {
const headers = [
		'Time',
		'Provider Name',
		'API Key',
		'Account',
		'Model',
		'Status',
		'Stream',
		'Latency',
		'Proxy',
		'Input',
		'Output',
		'Cached',
		'Cost',
		'Error',
	];
	const rows = $logs.map((row) => [
		formatLogTime(row.timestamp),
		row.provider_name || providerMeta(row.provider_type_id).displayName,
		row.api_key || '',
		row.connection_name || row.connection_id || '',
		row.model_id,
		row.status_code?.toString() || '',
		row.stream ? 'stream' : 'json',
		`${row.latency_ms}ms`,
		row.proxy_pool_id ? row.proxy_pool_name || row.proxy_pool_id : 'direct',
		row.input_tokens.toString(),
		row.output_tokens.toString(),
		row.cached_tokens.toString(),
		`$${row.cost_usd.toFixed(4)}`,
		row.error_message || '',
	]);
		const csv = [headers.join(','), ...rows.map((r) => r.join(','))].join('\n');
		const blob = new Blob([csv], { type: 'text/csv' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `axonrouter-logs-${new Date().toISOString().slice(0, 10)}.csv`;
		a.click();
		URL.revokeObjectURL(url);
	}

	function providerMeta(id: string | undefined | null) {
		const safeId = id ?? 'unknown';
		return (
			getProviderMeta(safeId) ?? {
				id: safeId,
				displayName: safeId,
				icon: 'network',
				textIcon: safeId.slice(0, 2).toUpperCase(),
				color: '#a1a1aa',
				category: 'compatible',
				description: '',
				format: 'openai',
				authType: 'apikey',
				prefix: `${safeId}/`,
				isBuiltIn: false,
			}
		);
	}

	function extractErrorCodeFromMessage(msg?: string): number | null {
		if (!msg) return null;
		const m = msg.match(/^stream error (\d+):/);
		return m ? parseInt(m[1], 10) : null;
	}
	function getStatusBadgeProps(
		statusCode: number | null | undefined,
		errorMsg?: string,
		category?: string
	) {
		const msgCode = extractErrorCodeFromMessage(errorMsg);
		if (msgCode && (typeof statusCode !== 'number' || statusCode <= 0)) {
			statusCode = msgCode;
		}
		if (typeof statusCode === 'number' && statusCode >= 200 && statusCode < 300)
			return { label: `${statusCode} OK`, variant: 'default' as const };
		if (statusCode === 429) {
			if (category === 'quota') return { label: '429 EXHAUSTED', variant: 'destructive' as const };
			if (category === 'rate_limit') return { label: '429 COOLDOWN', variant: 'secondary' as const };
			return { label: '429 RATE LIMITED', variant: 'secondary' as const };
		}
		if (statusCode === 401) return { label: '401 AUTH', variant: 'destructive' as const };
		if (typeof statusCode === 'number' && statusCode >= 400 && statusCode < 500)
			return { label: `${statusCode} CLIENT ERR`, variant: 'secondary' as const };
		if (typeof statusCode === 'number' && statusCode >= 500)
			return { label: `${statusCode} SERVER ERR`, variant: 'destructive' as const };
		if (errorMsg) return { label: 'ERROR', variant: 'destructive' as const };
		return { label: '—', variant: 'outline' as const };
	}
	function formatCooldown(cd?: number) {
		if (!cd) return '';
		const left = cd - Date.now();
		if (left <= 0) return '';
		const mins = Math.ceil(left / 60000);
		if (mins >= 60) {
			const h = Math.floor(mins / 60);
			const m = mins % 60;
			return m ? `${h}h${m}m` : `${h}h`;
		}
		return `${mins}m`;
	}

	let hasActiveFilters = $derived(
		$logFilter.provider_id ||
			$logFilter.connection_id ||
			$logFilter.model_id ||
			$logFilter.status_code ||
			$logFilter.start_date ||
			$logFilter.end_date
	);

	const statusPills = [
		{ code: 0, label: 'All' },
		{ code: 200, label: '200 OK' },
		{ code: 429, label: '429 Limited' },
		{ code: 401, label: '401 Auth' },
		{ code: 500, label: 'Error' },
	];

type ColumnDef = { key: string; label: string; subLabel?: string };

const columns: ColumnDef[] = [
	{ key: 'timestamp', label: 'Time' },
	{ key: 'provider_name', label: 'Provider Name', subLabel: 'API Key' },
	{ key: 'connection_name', label: 'Account', subLabel: 'Model' },
	{ key: 'status_code', label: 'Status Code', subLabel: 'Stream / JSON' },
	{ key: 'latency_ms', label: 'Latency', subLabel: 'Proxy' },
	{ key: 'tokens', label: 'Tokens' },
	{ key: 'cost_usd', label: 'Cost' },
	{ key: 'error_message', label: 'Error' },
];
</script>

<div class="flex flex-1 flex-col gap-6 p-6 w-full">
	<div class="flex items-center justify-between">
		<div class="space-y-1">
			<h1 class="text-display-lg">Logs.</h1>
			<p class="text-body-sm text-muted-foreground">
				Request tracing, latency tracking, and token usage analytics.
			</p>
		</div>
		<div class="flex items-center gap-2">
			<Button
				onclick={handleExport}
				disabled={$logs.length === 0}
				variant="outline"
				size="sm"
				class="text-body-sm h-9 rounded-sm"
			>
				<DownloadIcon class="size-3.5 mr-1.5" />
				Export CSV
			</Button>
			<Button
				onclick={() => loadLogs(currentPage, perPage)}
				disabled={$isLoading}
				variant="outline"
				size="sm"
				class="text-body-sm h-9 rounded-sm"
			>
				<RefreshCwIcon class="size-3.5 mr-1.5 {$isLoading ? 'animate-spin' : ''}" />
				Refresh
			</Button>
		</div>
	</div>

	<Card class="shadow-card">
		<CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between space-y-0">
			<div class="flex items-center gap-2">
				<FilterIcon class="size-4 text-muted-foreground" />
				<CardTitle class="text-body-md-strong">Filters</CardTitle>
				{#if hasActiveFilters}
					<Button
						onclick={clearFilters}
						variant="ghost"
						size="sm"
						class="text-caption-mono h-6 px-2 text-muted-foreground"
					>
						Clear all
					</Button>
				{/if}
			</div>
			<div class="flex items-center gap-1.5 overflow-x-auto">
				{#each statusPills as pill}
					<button
						class="rounded-sm px-3 py-1 text-caption-mono transition-colors {$logFilter.status_code ===
						pill.code
							? 'bg-foreground text-background'
							: 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
						onclick={() => setStatusFilter(pill.code)}
					>
						{pill.label}
					</button>
				{/each}
			</div>
		</CardHeader>
		<CardContent class="pt-4">
			<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
				<div class="space-y-1.5">
					<label for="filter-provider" class="text-caption-mono text-muted-foreground uppercase font-semibold">Provider</label>
					<Input
						id="filter-provider"
						placeholder="openai, cx, mimo..."
						class="h-9 font-mono text-body-sm"
						bind:value={$logFilter.provider_id}
						oninput={handleFilterChange}
					/>
				</div>
				<div class="space-y-1.5">
					<label for="filter-model" class="text-caption-mono text-muted-foreground uppercase font-semibold">Model</label>
					<Input
						id="filter-model"
						placeholder="gpt-4o, claude-sonnet..."
						class="h-9 font-mono text-body-sm"
						bind:value={$logFilter.model_id}
						oninput={handleFilterChange}
					/>
				</div>
				<div class="space-y-1.5">
					<label for="filter-start" class="text-caption-mono text-muted-foreground uppercase font-semibold">From date</label>
					<Input
						id="filter-start"
						type="date"
						class="h-9 font-mono text-body-sm"
						bind:value={$logFilter.start_date}
						oninput={handleFilterChange}
					/>
				</div>
				<div class="space-y-1.5">
					<label for="filter-end" class="text-caption-mono text-muted-foreground uppercase font-semibold">To date</label>
					<Input
						id="filter-end"
						type="date"
						class="h-9 font-mono text-body-sm"
						bind:value={$logFilter.end_date}
						oninput={handleFilterChange}
					/>
				</div>
			</div>
		</CardContent>
	</Card>

	{#if $activeRequests.length > 0}
		<Card class="shadow-card overflow-hidden">
			<CardContent class="p-0 flex justify-center bg-gradient-to-b from-background to-card">
				<ActiveOctopus requests={$activeRequests} />
			</CardContent>
		</Card>

		<Card class="shadow-card border-l-4 border-l-amber-500">
			<CardHeader class="pb-3 flex flex-row items-center justify-between space-y-0">
				<div class="flex items-center gap-2">
					<ActivityIcon class="size-4 text-amber-500" />
					<CardTitle class="text-body-md-strong">In-flight requests</CardTitle>
				</div>
				<p class="text-caption text-muted-foreground">{$activeRequests.length} active</p>
			</CardHeader>
			<CardContent class="p-0">
				<div class="overflow-x-auto">
					<table class="w-full text-left border-collapse">
						<thead>
							<tr class="border-b border-border bg-muted/30">
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-4">Started</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-4">Provider</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-4">Account</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-4">Model</th>
								<th class="text-caption-mono text-muted-foreground uppercase font-semibold py-2 px-4">Type</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-border/60">
							{#each $activeRequests as ar}
								<tr class="transition-colors hover:bg-accent/20">
									<td class="py-2 px-4 text-body-sm text-muted-foreground">{formatDurationMs(ar.started_at)}</td>
<td class="py-2 px-4 text-body-sm-strong">{providerMeta(ar.provider_type_id).displayName}</td>
									<td class="py-2 px-4 text-body-sm text-foreground">{ar.connection_name || ar.connection_id || '-'}</td>
									<td class="py-2 px-4 text-code text-foreground truncate max-w-[220px]" title={ar.model_id}>{ar.model_id}</td>
									<td class="py-2 px-4">
										<Badge variant={ar.stream ? 'default' : 'secondary'} class="text-caption-mono rounded-sm py-0.5">
											{ar.stream ? 'stream' : 'json'}
										</Badge>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</CardContent>
		</Card>
	{/if}

	{#if $isLoading}
		<div class="flex flex-col gap-3">
			{#each Array(8) as _}
				<div class="h-12 bg-card/60 animate-pulse rounded-md"></div>
			{/each}
		</div>
	{:else if $error}
		<Card class="shadow-card">
			<CardContent class="flex flex-col items-center justify-center py-16 text-center">
				<p class="text-destructive font-medium text-body-sm mb-4">{$error}</p>
				<Button onclick={() => loadLogs(currentPage, perPage)} variant="outline" size="sm" class="text-body-sm">Retry</Button>
			</CardContent>
		</Card>
	{:else if $logs.length === 0}
		<Card class="shadow-card">
			<CardContent class="flex flex-col items-center justify-center py-16 text-center">
				<TerminalIcon class="size-8 text-muted-foreground mb-3" />
				<p class="text-foreground font-semibold text-body-sm mb-1">No logs found.</p>
				<p class="text-muted-foreground text-body-sm">Adjust filters or send requests to the proxy to generate logs.</p>
			</CardContent>
		</Card>
	{:else}
		<Card class="shadow-card overflow-hidden">
			<CardContent class="p-0">
				<div class="overflow-x-auto">
					<table class="w-full text-left border-collapse">
						<thead>
							<tr class="border-b border-border bg-muted/30">
{#each columns as column}
						<th
							class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 align-bottom {column.key === 'timestamp'
							? 'min-w-[180px]'
							: ''} {column.key === 'provider_name' ? 'min-w-[180px]' : ''} {column.key === 'connection_name'
							? 'min-w-[220px]'
							: ''} {column.key === 'status_code' ? 'min-w-[140px]' : ''} {column.key === 'tokens'
							? 'text-right min-w-[120px]'
							: ''} {column.key === 'cost_usd' ? 'text-right min-w-[80px]' : ''} {column.key ===
							'error_message'
							? 'min-w-[160px]'
							: ''}"
						>
						<div class="flex flex-col leading-none">
							<span>{column.label}</span>
							{#if column.subLabel}
								<span class="text-[10px] normal-case text-muted-foreground/70">{column.subLabel}</span>
							{/if}
						</div>
						</th>
					{/each}
							</tr>
						</thead>
						<tbody class="divide-y divide-border/60">
							{#each $logs as row}
								{@const statusProps = getStatusBadgeProps(row.status_code, row.error_message, row.error_category)}
								<tr class="transition-colors hover:bg-accent/20">
									<td class="py-3 px-4 font-mono text-caption text-muted-foreground whitespace-nowrap">{formatLogTime(row.timestamp)}</td>
<td class="py-3 px-4">
							<div class="flex items-center gap-2.5">
								<ProviderIcon meta={providerMeta(row.provider_type_id)} size={24} />
								<div class="flex flex-col min-w-0">
									<a
										href="/providers/{row.provider_type_id}"
										class="text-body-sm-strong hover:underline text-foreground leading-none truncate"
									>{row.provider_name || providerMeta(row.provider_type_id).displayName}</a
									>
									<span class="text-caption-mono text-muted-foreground truncate" title={row.api_key || ''}>{row.api_key || '—'}</span>
								</div>
							</div>
						</td>
<td class="py-3 px-4">
							<div class="flex flex-col">
								<span class="text-body-sm text-foreground truncate" title={row.connection_name || row.connection_id || ''}>{row.connection_name || row.connection_id || '—'}</span>
								<span class="text-code text-muted-foreground truncate max-w-[220px]" title={row.model_id}>{row.model_id}</span>
							</div>
						</td>
<td class="py-3 px-4">
							<div class="flex flex-col gap-1">
								<Badge variant={statusProps.variant} class="text-caption-mono rounded-sm py-0.5 w-fit">{statusProps.label}</Badge>
								{#if row.cooldown_until && formatCooldown(row.cooldown_until)}
									<span class="text-caption-mono text-amber-500">cooldown {formatCooldown(row.cooldown_until)}</span>
								{/if}
								<Badge variant="outline" class="text-caption-mono rounded-sm py-0 w-fit border-border text-muted-foreground">{row.stream ? 'stream' : 'json'}</Badge>
							</div>
						</td>
          <td class="py-3 px-4">
            <div class="flex flex-col">
              <span class="text-code text-muted-foreground">{formatLatency(row.latency_ms)}</span>
              <span class="text-caption-mono text-muted-foreground">{row.proxy_pool_id ? row.proxy_pool_name || row.proxy_pool_id : 'direct'}</span>
            </div>
          </td>
<td class="py-3 px-4 text-code text-right whitespace-nowrap">
							<div class="flex flex-col items-end gap-1">
								{#if row.api_type}
									<span class="text-caption-mono text-muted-foreground">{row.api_type}</span>
								{/if}
								<div>
									<span class="text-muted-foreground">{formatTokens(row.input_tokens)}</span>
									<span class="text-muted-foreground/60">/</span>
									<span class="text-foreground">{formatTokens(row.output_tokens)}</span>
									<span class="text-muted-foreground/60">/</span>
									<span class="text-muted-foreground">{formatTokens(row.cached_tokens)}</span>
								</div>
								{#if row.tokens_estimated}
									<Badge variant="outline" class="text-caption-mono rounded-sm py-0 w-fit text-muted-foreground">est</Badge>
								{/if}
							</div>
						</td>
									<td class="py-3 px-4 text-code text-foreground font-medium text-right">{formatCost(row.cost_usd)}</td>
									<td class="py-3 px-4 text-caption">
										{#if row.error_message && row.error_message !== ''}
											<Button
												variant="outline"
												size="sm"
												onclick={() => (selectedErrorLog = row)}
												class="text-caption-mono h-7 px-2 py-0 border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive"
											>
												View Error
											</Button>
										{:else}
											<span class="text-muted-foreground">—</span>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</CardContent>
		</Card>

		<Pagination
			page={currentPage}
			totalPages={$logPagination.total_pages}
			total={$logPagination.total}
			perPage={perPage}
			perPageOptions={[50, 100, 200]}
			onPerPageChange={handlePerPageChange}
			onChange={handlePageChange}
		/>
	{/if}
</div>

<Dialog.Root
	open={selectedErrorLog !== null}
	onOpenChange={(o) => {
		if (!o) selectedErrorLog = null;
	}}
>
	<Dialog.Content class="sm:max-w-[640px] max-h-[90vh] overflow-hidden flex flex-col">
		<Dialog.Header>
			<Dialog.Title class="text-lg font-semibold">Request Error</Dialog.Title>
			<Dialog.Description class="text-sm text-muted-foreground">
				{selectedErrorLog?.provider_type_id || '—'} / {selectedErrorLog?.model_id || '—'} at {selectedErrorLog
					? formatLogTime(selectedErrorLog.timestamp)
						: ''}
			</Dialog.Description>
		</Dialog.Header>
		<div class="flex-1 overflow-auto my-4">
			<pre class="whitespace-pre-wrap break-words font-mono text-xs text-foreground bg-muted/40 p-4 rounded-sm">{selectedErrorLog?.error_message || ''}</pre>
		</div>
		<Dialog.Footer>
			<Button variant="outline" size="sm" onclick={() => (selectedErrorLog = null)}>Close</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
