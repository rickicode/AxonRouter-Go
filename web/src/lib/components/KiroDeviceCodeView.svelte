<script lang="ts">
import { Button } from '$lib/components/ui/button';
import { oauthApi } from '$lib/api';
import { toast } from 'svelte-sonner';
import { copyToClipboard } from '$lib/copy';
import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
import CopyIcon from '@lucide/svelte/icons/copy';
import Loader2Icon from '@lucide/svelte/icons/loader-2';

interface Props {
	authUrl: string;
	userCode?: string;
	sessionId: string;
	onSuccess: (connectionId: string, name?: string) => void;
	onError: (error: string) => void;
}

let { authUrl, userCode, sessionId, onSuccess, onError }: Props = $props();

let statusText = $state('Waiting for browser authorization...');
let polling = $state(true);
let codeEl = $state<HTMLElement | null>(null);

function handleStatus(status: Awaited<ReturnType<typeof oauthApi.pollKiroSession>>) {
	if (status.status === 'connected') {
		polling = false;
		const name = status.name || 'Kiro';
		statusText = `Connected as ${name}`;
		toast.success(`Kiro connected: ${name}`);
		onSuccess(status.connection_id || '', name);
	} else if (status.status === 'failed') {
		polling = false;
		const err = status.error || 'Kiro authentication failed';
		statusText = err;
		toast.error(err);
		onError(err);
	}
}

$effect(() => {
	if (!polling) return;

	oauthApi.pollKiroSession(sessionId).then(handleStatus).catch(() => {});

	const interval = setInterval(() => {
		oauthApi.pollKiroSession(sessionId).then(handleStatus).catch(() => {});
	}, 5000);

	return () => clearInterval(interval);
});

async function copyCode() {
	if (!userCode || !codeEl) return;
	await copyToClipboard(userCode, 'Code', codeEl);
}

function openAuthPage() {
	if (authUrl) window.open(authUrl, '_blank');
}
</script>

<div class="flex flex-col gap-4">
	<div class="flex items-center gap-3">
		<Loader2Icon class="size-5 animate-spin text-primary" />
		<p class="text-body-sm text-muted-foreground">{statusText}</p>
	</div>

	{#if userCode}
		<div class="rounded-xl border border-border bg-card p-4 text-center shadow-card">
			<p class="text-caption text-muted-foreground mb-1">Your device code</p>
			<p bind:this={codeEl} class="text-display-sm tracking-widest text-card-foreground select-all">{userCode}</p>
			<Button variant="outline" size="sm" class="mt-2 gap-1.5 text-body-sm rounded-sm cursor-pointer" onclick={copyCode}>
				<CopyIcon class="size-3.5" />
				Copy code
			</Button>
		</div>
	{/if}

	{#if authUrl}
		<div class="rounded-xl border border-border bg-card p-3 shadow-card">
			<p class="text-caption text-muted-foreground mb-2">Authorization URL</p>
			<p class="break-all font-mono text-code text-card-foreground select-all">{authUrl}</p>
		</div>

		<Button class="w-full gap-2 text-body-sm rounded-sm cursor-pointer" onclick={openAuthPage}>
			<ExternalLinkIcon class="size-4" />
			Open authorization page
		</Button>
	{/if}
</div>
