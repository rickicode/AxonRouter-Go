<script lang="ts">
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { oauthApi } from '$lib/api';
import { toast } from 'svelte-sonner';
import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
import SendIcon from '@lucide/svelte/icons/send';

interface Props {
	provider: 'google' | 'github';
	authUrl: string;
	sessionId: string;
	onSuccess: (connectionId: string, name?: string) => void;
	onError: (error: string) => void;
}

let { provider, authUrl, sessionId, onSuccess, onError }: Props = $props();

let pastedCode = $state('');
let submitting = $state(false);

function extractCode(value: string): string | null {
	const trimmed = value.trim();
	if (!trimmed) return null;
	try {
		const url = new URL(trimmed);
		return url.searchParams.get('code') || trimmed;
	} catch {
		return trimmed;
	}
}

function openAuthPage() {
	if (authUrl) window.open(authUrl, '_blank');
}

async function submitCode() {
	const code = extractCode(pastedCode);
	if (!code) {
		toast.error('Paste the redirect URL or code first');
		return;
	}
	submitting = true;
	try {
		const res = await oauthApi.exchangeKiroSocial(provider, sessionId, code);
		toast.success(`Kiro ${provider} connected: ${res.name}`);
		onSuccess(res.connection_id, res.name);
	} catch (err) {
		const msg = err instanceof Error ? err.message : 'Social login failed';
		toast.error(`Kiro ${provider} login failed: ${msg}`);
		onError(msg);
	} finally {
		submitting = false;
	}
}
</script>

<div class="flex flex-col gap-4">
	<div class="rounded-xl border border-border bg-card p-3 shadow-card">
		<p class="text-caption text-muted-foreground mb-2">Authorization URL</p>
		<p class="break-all font-mono text-code text-card-foreground select-all">{authUrl}</p>
	</div>

	<Button class="w-full gap-2 text-body-sm rounded-sm cursor-pointer" onclick={openAuthPage}>
		<ExternalLinkIcon class="size-4" />
		Open {provider} authorization page
	</Button>

	<div class="flex flex-col gap-1.5">
		<Label for="kiro-social-code" class="text-body-sm-strong">Paste redirect URL or code</Label>
		<Input
			id="kiro-social-code"
			bind:value={pastedCode}
			placeholder="kiro://auth/callback?code=..."
			class="h-9 font-mono text-code"
			autocomplete="off"
			spellcheck={false}
		/>
	</div>

	<Button class="w-full gap-2 text-body-sm rounded-sm cursor-pointer" disabled={submitting} onclick={submitCode}>
		<SendIcon class="size-4" />
		{submitting ? 'Submitting...' : 'Submit'}
	</Button>
</div>
