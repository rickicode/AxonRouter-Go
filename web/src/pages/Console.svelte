<script lang="ts">
import { onMount, tick } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Badge } from '$lib/components/ui/badge';
import { ScrollArea } from '$lib/components/ui/scroll-area';
import { getConsoleLogs, type ConsoleLogEntry, type ConsoleLogsResponse } from '$lib/api';
import { toast } from 'svelte-sonner';
import TerminalIcon from '@lucide/svelte/icons/terminal';
import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
import PauseIcon from '@lucide/svelte/icons/pause';
import PlayIcon from '@lucide/svelte/icons/play';
import CopyIcon from '@lucide/svelte/icons/copy';
import CheckIcon from '@lucide/svelte/icons/check';
import SearchIcon from '@lucide/svelte/icons/search';
import FilterIcon from '@lucide/svelte/icons/filter';

let logs = $state<ConsoleLogsResponse | null>(null);
let isLoading = $state(false);
let isPaused = $state(false);
let logViewport = $state<HTMLPreElement | null>(null);
let pollTimer = $state<ReturnType<typeof setInterval> | null>(null);
let lastError = $state<string | null>(null);
let levelFilter = $state('debug');
let searchQuery = $state('');
let copiedIndex = $state<number | null>(null);

const levels = [
	{ value: 'debug', label: 'All', color: 'text-muted-foreground' },
	{ value: 'info', label: 'Info+', color: 'text-blue-400' },
	{ value: 'warn', label: 'Warn+', color: 'text-amber-400' },
	{ value: 'error', label: 'Error+', color: 'text-red-400' },
];

async function fetchLogs(immediate = false) {
	if (isLoading && !immediate) return;
	isLoading = true;
	try {
		const result = await getConsoleLogs({
			level: levelFilter,
			search: searchQuery || undefined,
		});
		logs = result;
		lastError = null;
		await tick();
		scrollToBottom();
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

function scrollToBottom() {
	if (logViewport) {
		logViewport.scrollTop = logViewport.scrollHeight;
	}
}

function startPolling() {
	if (pollTimer) return;
	pollTimer = setInterval(() => fetchLogs(), 3000);
}

function stopPolling() {
	if (pollTimer) {
		clearInterval(pollTimer);
		pollTimer = null;
	}
}

function togglePause() {
	isPaused = !isPaused;
	if (isPaused) {
		stopPolling();
		toast.info('Console log polling paused');
	} else {
		startPolling();
		fetchLogs(true);
		toast.info('Console log polling resumed');
	}
}

function handleRefresh() {
	fetchLogs(true);
}

function handleLevelChange(level: string) {
	levelFilter = level;
	fetchLogs(true);
}

let searchDebounce: ReturnType<typeof setTimeout> | null = null;
function handleSearchInput(value: string) {
	searchQuery = value;
	if (searchDebounce) clearTimeout(searchDebounce);
	searchDebounce = setTimeout(() => fetchLogs(true), 300);
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

function getLevelBg(level: string): string {
	switch (level) {
		case 'debug':
			return 'bg-slate-500/10';
		case 'info':
			return 'bg-blue-500/10';
		case 'warn':
			return 'bg-amber-500/10';
		case 'error':
			return 'bg-red-500/10';
		case 'fatal':
			return 'bg-red-500/20';
		default:
			return '';
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
		return d.toLocaleTimeString('en-US', {
			hour12: false,
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit',
		}) + '.' + String(d.getMilliseconds()).padStart(3, '0');
	} catch {
		return ts;
	}
}

function truncateCID(cid: string): string {
	return cid.length > 8 ? cid.slice(0, 8) : cid;
}

onMount(() => {
	document.title = 'Console — AxonRouter';
	fetchLogs(true);
	startPolling();
	return () => {
		stopPolling();
		if (searchDebounce) clearTimeout(searchDebounce);
	};
});
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="flex items-center justify-between">
		<div class="space-y-1">
			<h1 class="text-display-lg">Console.</h1>
			<p class="text-body-sm text-muted-foreground">
				Structured application log viewer{logs?.path ? ` — ${logs.path}` : ''}.
			</p>
		</div>
		<div class="flex items-center gap-2">
			<Badge variant={isPaused ? 'secondary' : 'default'} class="text-caption-mono rounded-sm">
				{isPaused ? 'Paused' : 'Live'}
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
		</div>
	</div>

	<!-- Filters -->
	<Card class="shadow-card">
		<CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between space-y-0">
			<div class="flex items-center gap-2">
				<FilterIcon class="size-4 text-muted-foreground" />
				<CardTitle class="text-body-md-strong">Filters</CardTitle>
			</div>
			<div class="flex items-center gap-1.5">
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

	<!-- Log entries -->
	<Card class="shadow-card flex flex-1 min-h-0">
		<CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between space-y-0 shrink-0">
			<div class="flex items-center gap-2">
				<TerminalIcon class="size-4 text-muted-foreground" />
				<CardTitle class="text-body-md-strong">Application log</CardTitle>
			</div>
			{#if logs}
				<p class="text-caption text-muted-foreground">
					{logs.total} entr{logs.total === 1 ? 'y' : 'ies'}
				</p>
			{/if}
		</CardHeader>
		<CardContent class="flex-1 p-0 min-h-0">
			<ScrollArea class="h-full">
				<div class="min-h-full" style="background: #0d1117;">
					<!-- Terminal header -->
					<div class="flex items-center gap-1.5 px-4 py-2 border-b border-white/5">
						<div class="w-2.5 h-2.5 rounded-full bg-red-500/80"></div>
						<div class="w-2.5 h-2.5 rounded-full bg-amber-500/80"></div>
						<div class="w-2.5 h-2.5 rounded-full bg-green-500/80"></div>
						<span class="ml-2 text-caption text-white/30 font-mono">console</span>
						<div class="flex-1"></div>
						{#if !isPaused}
							<div class="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse"></div>
						{/if}
					</div>

					{#if !logs || logs.entries.length === 0}
						<div class="flex flex-col items-center justify-center py-16 text-center">
							<TerminalIcon class="size-8 text-white/20 mb-3" />
							<p class="text-white/40 text-body-sm">No log entries found.</p>
							<p class="text-white/20 text-caption mt-1">
								{#if searchQuery || levelFilter !== 'debug'}
									Try adjusting filters.
								{:else}
									Logs will appear here once the application starts logging.
								{/if}
							</p>
						</div>
					{:else}
						{#each logs.entries as entry, i}
							<div
								class="group flex items-start gap-0 px-4 py-1.5 border-b border-white/5 hover:bg-white/5 transition-colors {getLevelBg(entry.level)}"
							>
								<!-- Timestamp -->
								<span class="text-caption font-mono text-white/30 whitespace-nowrap min-w-[95px] shrink-0">
									{formatTimestamp(entry.ts)}
								</span>

								<!-- Level badge -->
								<span class="text-caption font-mono min-w-[50px] shrink-0 text-right {getLevelColor(entry.level)}">
									{entry.level.toUpperCase().padEnd(5)}
								</span>

								<!-- Component tag -->
								{#if entry.component}
									<span class="text-caption font-mono text-purple-400 shrink-0 ml-2">
										[{entry.component}]
									</span>
								{/if}

								<!-- Request ID -->
								{#if entry.request_id}
									<span class="text-caption font-mono text-cyan-400/60 shrink-0 ml-2" title={entry.request_id}>
										cid:{truncateCID(entry.request_id)}
									</span>
								{/if}

								<!-- Message -->
								<span class="text-caption font-mono text-white/80 ml-3 break-words flex-1 min-w-0">
									{entry.msg}
								</span>

								<!-- Extra metadata chips -->
								{#if entry.provider || entry.model || entry.conn}
									<div class="flex items-center gap-1 ml-3 shrink-0">
										{#if entry.provider}
											<span class="text-[10px] font-mono px-1.5 py-0.5 rounded bg-white/5 text-white/40">
												{entry.provider}
											</span>
										{/if}
										{#if entry.model}
											<span class="text-[10px] font-mono px-1.5 py-0.5 rounded bg-white/5 text-amber-400/60">
												{entry.model}
											</span>
										{/if}
									</div>
								{/if}

								<!-- Copy button (hidden until hover) -->
								<button
									class="opacity-0 group-hover:opacity-100 transition-opacity ml-2 shrink-0 p-1 rounded hover:bg-white/10"
									onclick={() => copyEntry(entry, i)}
									title="Copy entry as JSON"
								>
									{#if copiedIndex === i}
										<CheckIcon class="size-3 text-green-400" />
									{:else}
										<CopyIcon class="size-3 text-white/30" />
									{/if}
								</button>
							</div>
						{/each}
					{/if}
				</div>
			</ScrollArea>
		</CardContent>
	</Card>
</div>
