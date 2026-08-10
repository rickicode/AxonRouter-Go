<script lang="ts">
import { onMount } from 'svelte';
import { router } from '$lib/router';
import { Skeleton } from '$lib/components/ui/skeleton';
import { toast } from 'svelte-sonner';
import { cliToolsApi } from '$lib/api';
import type { CLITool, CLIToolStatus } from '$lib/api';
import { CLIToolCard } from '$lib/components/cli-tools';

let tools = $state<CLITool[]>([]);
let statuses = $state<Record<string, CLIToolStatus>>({});
let loading = $state(true);
let pageError = $state<string | null>(null);

onMount(() => {
	document.title = 'CLI Tools — AxonRouter';
	loadAll();
});

async function loadAll() {
	loading = true;
	pageError = null;
	try {
		const [toolsRes, statusRes] = await Promise.all([
			cliToolsApi.list(),
			cliToolsApi.statuses(),
		]);
		tools = toolsRes.data ?? [];
		statuses = statusRes ?? {};
	} catch (err) {
		pageError = err instanceof Error ? err.message : 'Failed to load CLI tools';
		toast.error(pageError);
	} finally {
		loading = false;
	}
}

function selectTool(tool: CLITool) {
	router.navigate(`/cli-tools/${tool.id}`);
}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
	<div class="space-y-1">
		<h1 class="text-display-lg">CLI Tools.</h1>
		<p class="text-body-sm text-muted-foreground">
			Pick a gateway model and API key, then generate ready-to-use config snippets for popular AI
			CLIs.
		</p>
	</div>
	{#if pageError}
		<div class="rounded-xl border border-destructive/30 bg-destructive/10 p-4 text-body-sm text-destructive">
			{pageError}
		</div>
	{/if}
	{#if loading}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
			{#each Array(6) as _}
				<Skeleton class="h-32 rounded-xl" />
			{/each}
		</div>
	{:else}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
			{#each tools as tool (tool.id)}
				<CLIToolCard {tool} status={statuses[tool.id]} onClick={() => selectTool(tool)} />
			{/each}
		</div>
	{/if}
</div>
