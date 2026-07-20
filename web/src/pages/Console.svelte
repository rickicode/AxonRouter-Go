<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { ScrollArea } from '$lib/components/ui/scroll-area';
	import { getConsoleLogs, type ConsoleLogsResponse } from '$lib/api';
	import { toast } from 'svelte-sonner';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import PauseIcon from '@lucide/svelte/icons/pause';
	import PlayIcon from '@lucide/svelte/icons/play';

	let logs = $state<ConsoleLogsResponse | null>(null);
	let isLoading = $state(false);
	let isPaused = $state(false);
	let logViewport = $state<HTMLPreElement | null>(null);
	let pollTimer = $state<ReturnType<typeof setInterval> | null>(null);
	let lastError = $state<string | null>(null);

	async function fetchLogs(immediate = false) {
		if (isLoading && !immediate) return;
		isLoading = true;
		try {
			const result = await getConsoleLogs();
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
		pollTimer = setInterval(() => fetchLogs(), 2000);
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

	onMount(() => {
		document.title = 'Console — AxonRouter';
		fetchLogs(true);
		startPolling();
		return () => {
			stopPolling();
		};
	});

	const logText = $derived((logs?.lines ?? []).join('\n'));
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="flex items-center justify-between">
		<div class="space-y-1">
			<h1 class="text-display-lg">Console.</h1>
			<p class="text-body-sm text-muted-foreground">
				Live application log stream{logs?.path ? ` — ${logs.path}` : ''}.
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

	<Card class="shadow-card flex flex-1 min-h-0">
		<CardHeader class="pb-3 border-b border-border flex flex-row items-center justify-between space-y-0 shrink-0">
			<div class="flex items-center gap-2">
				<TerminalIcon class="size-4 text-muted-foreground" />
				<CardTitle class="text-body-md-strong">Application log</CardTitle>
			</div>
			{#if logs}
				<p class="text-caption text-muted-foreground">
					{logs.lines.length} line{logs.lines.length === 1 ? '' : 's'}
				</p>
			{/if}
		</CardHeader>
		<CardContent class="flex-1 p-0 min-h-0">
			<ScrollArea class="h-full">
				<pre
					bind:this={logViewport}
					class="font-mono text-body-sm whitespace-pre-wrap break-words p-4 min-h-full">{logText}</pre>
			</ScrollArea>
		</CardContent>
	</Card>
</div>
