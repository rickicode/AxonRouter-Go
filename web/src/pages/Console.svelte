<script lang="ts">
import { onMount, tick } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Badge } from '$lib/components/ui/badge';
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from '$lib/components/ui/table';
import {
	getConsoleLogs,
	streamConsoleLogs,
	clearConsoleLogs,
	type ConsoleLogEntry,
	type ConsoleLogsResponse,
} from '$lib/api';
import { toast } from 'svelte-sonner';
import TerminalIcon from '@lucide/svelte/icons/terminal';
import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
import PauseIcon from '@lucide/svelte/icons/pause';
import PlayIcon from '@lucide/svelte/icons/play';
import CopyIcon from '@lucide/svelte/icons/copy';
import CheckIcon from '@lucide/svelte/icons/check';
import SearchIcon from '@lucide/svelte/icons/search';
import FilterIcon from '@lucide/svelte/icons/filter';
import Trash2Icon from '@lucide/svelte/icons/trash-2';

const MAX_CONSOLE_LINES = 500;

let logs = $state<ConsoleLogsResponse>({ entries: [], path: '', total: 0 });
let isLoading = $state(false);
let isPaused = $state(false);
let logContainer = $state<HTMLDivElement | null>(null);
let pollTimer = $state<ReturnType<typeof setInterval> | null>(null);
let eventSource = $state<EventSource | null>(null);
let connectionMode = $state<'sse' | 'poll'>('sse');
let lastError = $state<string | null>(null);
let levelFilter = $state('debug');
let searchQuery = $state('');
let copiedIndex = $state<number | null>(null);
let isAtLiveEdge = $state(true);

const levels = [
	{ value: 'debug', label: 'All', color: 'text-muted-foreground' },
	{ value: 'info', label: 'Info+', color: 'text-blue-400' },
	{ value: 'warn', label: 'Warn+', color: 'text-amber-400' },
	{ value: 'error', label: 'Error+', color: 'text-red-400' },
];

type ColumnDef = { key: string; label: string; subLabel?: string; class?: string };
const columns: ColumnDef[] = [
	{ key: 'ts', label: 'Time' },
	{ key: 'level', label: 'Level' },
	{ key: 'component', label: 'Component' },
	{ key: 'source', label: 'Source', subLabel: 'Provider / Model' },
	{ key: 'request_id', label: 'Request' },
	{ key: 'message', label: 'Message' },
	{ key: 'details', label: 'Details', subLabel: 'HTTP / Extras' },
];

function entryKey(entry: ConsoleLogEntry): string {
	const parts = [
		entry.ts ?? '',
		entry.level ?? '',
		entry.component ?? '',
		entry.msg ?? '',
		entry.request_id ?? '',
		entry.provider ?? '',
		entry.model ?? '',
	];
	return parts.join('|');
}

function normalizeHttpStatus(value: unknown): number | null {
	if (value === null || value === undefined) return null;
	const n = typeof value === 'string' ? parseInt(value, 10) : Number(value);
	return Number.isFinite(n) ? n : null;
}

function getHttpStatusVariant(status: number | null): 'default' | 'secondary' | 'destructive' | 'outline' {
	if (status === null) return 'outline';
	if (status >= 200 && status < 300) return 'default';
	if (status >= 400 && status < 500) return 'secondary';
	if (status >= 500) return 'destructive';
	return 'outline';
}

function isHttpEntry(entry: ConsoleLogEntry): boolean {
	return entry.component?.toLowerCase() === 'http';
}

function mergeEntries(incoming: ConsoleLogEntry[]): boolean {
	// API returns oldest-first; the UI renders newest-first.
	const newestFirst = incoming.slice().reverse();
	if (logs.entries.length === 0) {
		logs.entries = newestFirst.slice(0, MAX_CONSOLE_LINES);
		logs.total = logs.entries.length;
		return newestFirst.length > 0;
	}
	const existingKeys = new Set(logs.entries.map(entryKey));
	const fresh = newestFirst.filter((e) => !existingKeys.has(entryKey(e)));
	if (fresh.length === 0) return false;
	logs.entries = [...fresh, ...logs.entries].slice(0, MAX_CONSOLE_LINES);
	logs.total = logs.entries.length;
	return true;
}

function appendEntry(entry: ConsoleLogEntry): boolean {
	const key = entryKey(entry);
	if (logs.entries.some((e) => entryKey(e) === key)) return false;
	logs.entries = [entry, ...logs.entries].slice(0, MAX_CONSOLE_LINES);
	logs.total = logs.entries.length;
	return true;
}

async function fetchLogs(immediate = false) {
	if (isLoading && !immediate) return;
	isLoading = true;
	try {
		const result = await getConsoleLogs({
			level: levelFilter,
			search: searchQuery || undefined,
		});
		logContainer ??= null;
		const changed = mergeEntries(result.entries);
		logs.path = result.path;
		lastError = null;
		if (changed) {
			await tick();
			if (isAtLiveEdge) scrollToLiveEdge();
		}
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Unknown error';
		if (lastError !== message) {
			toast.error('Failed to load console logs: ' + message);
			lastError = message;
		}
	} finally {
		isLoading = false;
	}
}

function scrollToLiveEdge() {
	if (logContainer) {
		logContainer.scrollTop = 0;
	}
}

function handleScroll() {
	if (!logContainer) return;
	const threshold = 50;
	isAtLiveEdge = logContainer.scrollTop <= threshold;
}

function startPolling() {
	if (pollTimer || isPaused) return;
	connectionMode = 'poll';
	pollTimer = setInterval(() => fetchLogs(), 3000);
	fetchLogs(true);
}

function stopPolling() {
	if (pollTimer) {
		clearInterval(pollTimer);
		pollTimer = null;
	}
}

function closeSSE() {
	if (eventSource) {
		eventSource.close();
		eventSource = null;
	}
}

function safeParse(data: string): unknown {
	try {
		return JSON.parse(data);
	} catch {
		return null;
	}
}

function applySSEEvent(type: 'init' | 'line' | 'clear', data: unknown) {
	if (type === 'clear') {
		logs.entries = [];
		logs.total = 0;
		return;
	}
	if (type === 'init') {
		const init = data as { entries?: ConsoleLogEntry[]; path?: string; total?: number } | null;
		const entries = init?.entries ?? [];
		mergeEntries(entries);
		logs.path = init?.path ?? logs.path;
		tick().then(() => {
			if (isAtLiveEdge) scrollToLiveEdge();
		});
		return;
	}
	if (type === 'line') {
		const entry = data as ConsoleLogEntry | null;
		if (!entry || !entry.level) return;
		if (appendEntry(entry)) {
			tick().then(() => {
				if (isAtLiveEdge) scrollToLiveEdge();
			});
		}
	}
}

function startSSE() {
	closeSSE();
	stopPolling();
	if (isPaused || typeof EventSource === 'undefined') {
		startPolling();
		return;
	}
	try {
		const es = streamConsoleLogs({
			level: levelFilter,
			search: searchQuery || undefined,
		});
		es.onopen = () => {
			connectionMode = 'sse';
			lastError = null;
		};
		es.addEventListener('init', (e) => {
			applySSEEvent('init', safeParse(e.data));
		});
		es.addEventListener('line', (e) => {
			applySSEEvent('line', safeParse(e.data));
		});
		es.addEventListener('clear', () => {
			applySSEEvent('clear', null);
		});
		es.onerror = () => {
			closeSSE();
			if (!isPaused) startPolling();
		};
		eventSource = es;
	} catch (err) {
		if (!isPaused) startPolling();
	}
}

function togglePause() {
	isPaused = !isPaused;
	if (isPaused) {
		stopPolling();
		closeSSE();
		toast.info('Console log streaming paused');
	} else {
		startSSE();
		toast.info('Console log streaming resumed');
	}
}

function handleRefresh() {
	if (connectionMode === 'sse' && eventSource) {
		startSSE();
	} else {
		fetchLogs(true);
	}
}

function handleLevelChange(level: string) {
	levelFilter = level;
	if (isPaused) return;
	if (connectionMode === 'sse') {
		startSSE();
	} else {
		stopPolling();
		startPolling();
	}
}

let searchDebounce: ReturnType<typeof setTimeout> | null = null;
function handleSearchInput(value: string) {
	searchQuery = value;
	if (searchDebounce) clearTimeout(searchDebounce);
	searchDebounce = setTimeout(() => {
		if (isPaused) return;
		if (connectionMode === 'sse') {
			startSSE();
		} else {
			stopPolling();
			startPolling();
		}
	}, 300);
}

async function handleClearLogs() {
	const confirmed = window.confirm(
		'Are you sure you want to clear all console logs? This action cannot be undone.',
	);
	if (!confirmed) return;
	try {
		await clearConsoleLogs();
		logs.entries = [];
		logs.total = 0;
		lastError = null;
		toast.success('Console logs cleared');
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Unknown error';
		toast.error('Failed to clear console logs: ' + message);
	}
}

async function copyEntry(entry: ConsoleLogEntry, index: number) {
	try {
		await navigator.clipboard.writeText(JSON.stringify(entry, null, 2));
		copiedIndex = index;
		setTimeout(() => (copiedIndex = null), 2000);
	} catch {
		toast.error('Failed to copy to clipboard');
	}
}

function getLevelColor(level: string): string {
	switch (level) {
		case 'debug':
			return 'text-slate-400';
		case 'info':
			return 'text-blue-400';
		case 'warn':
			return 'text-amber-400';
		case 'error':
			return 'text-red-400';
		case 'fatal':
			return 'text-red-500 font-bold';
		default:
			return 'text-muted-foreground';
	}
}

function getLevelBadgeVariant(level: string): 'default' | 'secondary' | 'destructive' | 'outline' {
	switch (level) {
		case 'error':
		case 'fatal':
			return 'destructive';
		case 'warn':
			return 'secondary';
		default:
			return 'outline';
	}
}

function formatTimestamp(ts: string): string {
	try {
		const d = new Date(ts);
		if (isNaN(d.getTime())) return ts;
		return (
			d.toLocaleTimeString('en-US', {
				hour12: false,
				hour: '2-digit',
				minute: '2-digit',
				second: '2-digit',
			}) +
			'.' +
			String(d.getMilliseconds()).padStart(3, '0')
		);
	} catch {
		return ts;
	}
}

function truncateCID(cid: string): string {
	return cid.length > 8 ? cid.slice(0, 8) : cid;
}

function formatExtraChips(entry: ConsoleLogEntry): string[] {
	if (!entry.extra) return [];
	return Object.entries(entry.extra)
		.filter(([k]) => !isHttpEntry(entry) || !['status', 'method', 'path', 'lat', 'client_ip', 'user_agent'].includes(k))
		.map(([k, v]) => `${k}=${String(v).slice(0, 60)}`);
}

onMount(() => {
	document.title = 'Console — AxonRouter';
	startSSE();
	return () => {
		stopPolling();
		closeSSE();
		if (searchDebounce) clearTimeout(searchDebounce);
	};
});
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="flex items-center justify-between">
		<div class="space-y-1">
			<h1 class="text-display-lg">Console.</h1>
			<p class="text-body-sm text-muted-foreground">
				Structured application log viewer{logs.path ? ` — ${logs.path}` : ''}.
			</p>
		</div>
		<div class="flex items-center gap-2">
			<Badge variant={isPaused ? 'secondary' : 'default'} class="text-caption-mono rounded-sm">
				{isPaused ? 'Paused' : connectionMode === 'sse' ? 'Live SSE' : 'Live Poll'}
			</Badge>
			<Button
				onclick={togglePause}
				variant="outline"
				size="sm"
				class="text-body-sm rounded-sm cursor-pointer"
			>
				{#if isPaused}
					<PlayIcon class="size-3.5 mr-1.5" />
					Resume
				{:else}
					<PauseIcon class="size-3.5 mr-1.5" />
					Pause
				{/if}
			</Button>
			<Button
				onclick={handleRefresh}
				disabled={isLoading}
				variant="outline"
				size="sm"
				class="text-body-sm rounded-sm cursor-pointer"
			>
				<RefreshCwIcon class="size-3.5 mr-1.5 {isLoading ? 'animate-spin' : ''}" />
				Refresh
			</Button>
			<Button
				onclick={handleClearLogs}
				variant="outline"
				size="sm"
				class="text-body-sm rounded-sm cursor-pointer text-red-400 hover:text-red-300 hover:bg-red-500/10"
			>
				<Trash2Icon class="size-3.5 mr-1.5" />
				Clear Logs
			</Button>
		</div>
	</div>

	<!-- Filters -->
	<Card class="shadow-card">
		<CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between space-y-0">
			<div class="flex items-center gap-2">
				<FilterIcon class="size-4 text-muted-foreground" />
				<CardTitle class="text-body-md-strong">Filters</CardTitle>
			</div>
			<div class="flex items-center gap-1.5 overflow-x-auto">
				{#each levels as level}
					<button
						class="rounded-sm px-3 py-1 text-caption-mono transition-colors {levelFilter === level.value
							? 'bg-foreground text-background'
							: 'text-muted-foreground hover:text-foreground hover:bg-accent/50'}"
						onclick={() => handleLevelChange(level.value)}
					>
						{level.label}
					</button>
				{/each}
			</div>
		</CardHeader>
		<CardContent class="pt-4">
			<div class="relative">
				<SearchIcon class="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
				<input
					type="text"
					placeholder="Search logs... (message, component, provider, model)"
					class="w-full h-9 pl-9 pr-4 rounded-sm border border-border bg-background font-mono text-body-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
					value={searchQuery}
					oninput={(e) => handleSearchInput((e.target as HTMLInputElement).value)}
				/>
			</div>
		</CardContent>
	</Card>

	<!-- Log table -->
	<Card class="shadow-card flex flex-1 min-h-0 overflow-hidden">
		<CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between space-y-0 shrink-0">
			<div class="flex items-center gap-2">
				<TerminalIcon class="size-4 text-muted-foreground" />
				<CardTitle class="text-body-md-strong">Application log</CardTitle>
			</div>
			<p class="text-caption text-muted-foreground">
				{logs.total} entr{logs.total === 1 ? 'y' : 'ies'}
			</p>
		</CardHeader>
		<CardContent class="flex-1 p-0 min-h-0">
			{#if logs.entries.length === 0}
				<div class="flex flex-col items-center justify-center py-16 text-center">
					<TerminalIcon class="size-8 text-muted-foreground mb-3" />
					<p class="text-foreground font-semibold text-body-sm mb-1">No log entries found.</p>
					<p class="text-muted-foreground text-body-sm">
						{#if searchQuery || levelFilter !== 'debug'}
							Try adjusting filters.
						{:else}
							Logs will appear here once the application starts logging.
						{/if}
					</p>
				</div>
			{:else}
				<div
					bind:this={logContainer}
					onscroll={handleScroll}
					class="h-full overflow-auto"
				>
					<Table>
						<TableHeader>
							<TableRow class="hover:bg-transparent">
								{#each columns as column}
									<TableHead class="{column.class ?? ''} {column.key === 'ts' ? 'min-w-[120px]' : ''} {column.key === 'level' ? 'w-[80px]' : ''} {column.key === 'component' ? 'min-w-[100px]' : ''} {column.key === 'source' ? 'min-w-[140px] md:min-w-[180px]' : ''} {column.key === 'request_id' ? 'min-w-[100px]' : ''} {column.key === 'message' ? 'min-w-[200px] md:min-w-[280px]' : ''} {column.key === 'details' ? 'min-w-[180px] md:min-w-[220px]' : ''}">
										<div class="flex flex-col leading-none">
											<span>{column.label}</span>
											{#if column.subLabel}
												<span class="text-[10px] normal-case text-muted-foreground/70">{column.subLabel}</span>
											{/if}
										</div>
									</TableHead>
								{/each}
								<TableHead class="w-[50px]"></TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each logs.entries as entry, i (entryKey(entry))}
								{@const http = isHttpEntry(entry)}
								{@const status = http ? normalizeHttpStatus(entry.extra?.status) : null}
								<TableRow class="group font-mono">
									<TableCell class="text-caption text-muted-foreground whitespace-nowrap">{formatTimestamp(entry.ts)}</TableCell>
									<TableCell>
										<Badge variant={getLevelBadgeVariant(entry.level)} class="text-caption-mono rounded-sm">
											{entry.level.toUpperCase()}
										</Badge>
									</TableCell>
									<TableCell class="text-caption {getLevelColor(entry.level)}">
										{entry.component ?? '—'}
									</TableCell>
									<TableCell>
										<div class="flex flex-col">
											<span class="text-body-sm text-foreground truncate max-w-[160px] md:max-w-[220px]" title={entry.provider || ''}>
												{entry.provider || '—'}
											</span>
											<span class="text-code text-muted-foreground truncate max-w-[160px] md:max-w-[220px]" title={entry.model || ''}>
												{entry.model || (entry.conn ? `conn:${entry.conn}` : '—')}
											</span>
										</div>
									</TableCell>
									<TableCell class="text-caption-mono text-cyan-400/70 whitespace-nowrap" title={entry.request_id || ''}>
										{entry.request_id ? `cid:${truncateCID(entry.request_id)}` : '—'}
									</TableCell>
									<TableCell>
										{#if http}
											<div class="flex items-center gap-2 flex-wrap">
												<Badge variant={getHttpStatusVariant(status)} class="text-caption-mono rounded-sm">
													{status ?? '—'}
												</Badge>
												<span class="text-caption-mono text-muted-foreground">{String(entry.extra?.method ?? '—')}</span>
												<span class="text-caption text-foreground truncate max-w-[180px] md:max-w-[260px]" title={String(entry.extra?.path ?? '')}>
													{String(entry.extra?.path ?? '—')}
												</span>
												<span class="text-caption-mono text-muted-foreground">{String(entry.extra?.lat ?? '—')}</span>
											</div>
										{:else}
											<span class="text-caption text-foreground truncate max-w-[200px] md:max-w-[320px]" title={entry.msg}>{entry.msg}</span>
										{/if}
									</TableCell>
									<TableCell>
										{#if http}
											<div class="flex flex-col">
												<code class="text-caption-mono text-muted-foreground whitespace-nowrap">{String(entry.extra?.client_ip ?? '—')}</code>
												<span class="text-caption text-muted-foreground truncate max-w-[140px] md:max-w-[200px]" title={String(entry.extra?.user_agent ?? '')}>
													{String(entry.extra?.user_agent ?? '—')}
												</span>
											</div>
										{:else}
											<div class="flex flex-wrap gap-1">
												{#if entry.error}
													<span class="text-caption text-destructive truncate max-w-[200px] md:max-w-[300px]" title={entry.error}>{entry.error}</span>
												{/if}
												{#if entry.extra}
													{#each formatExtraChips(entry) as chip}
														<Badge variant="outline" class="text-[10px] font-mono rounded-sm py-0 text-muted-foreground border-border">{chip}</Badge>
													{/each}
												{/if}
											</div>
										{/if}
									</TableCell>
									<TableCell class="w-[50px]">
										<button
											class="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded hover:bg-accent"
											onclick={() => copyEntry(entry, i)}
											title="Copy entry as JSON"
										>
											{#if copiedIndex === i}
												<CheckIcon class="size-3 text-green-400" />
											{:else}
												<CopyIcon class="size-3 text-muted-foreground" />
											{/if}
										</button>
									</TableCell>
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				</div>
			{/if}
		</CardContent>
	</Card>
</div>
