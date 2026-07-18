<script lang="ts">
import { onMount } from 'svelte';
import { toast } from 'svelte-sonner';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import * as Card from '$lib/components/ui/card';
import { backupApi } from '$lib/api';
import DownloadIcon from '@lucide/svelte/icons/download';
import UploadIcon from '@lucide/svelte/icons/upload';
import DatabaseIcon from '@lucide/svelte/icons/database';
import ShieldIcon from '@lucide/svelte/icons/shield';

let backupPassword = $state('');
let downloadLoading = $state(false);

let restoreFiles = $state<FileList>();
let restorePassword = $state('');
let restoreLoading = $state(false);
let restoreSummary = $state('');

const restoreFile = $derived(restoreFiles?.item(0) ?? null);
const canDownload = $derived(!downloadLoading);
const canRestore = $derived(!!restoreFile && !restoreLoading);

onMount(() => {
document.title = 'Backup & Restore — AxonRouter';
});

function backupFilename() {
const date = new Date().toISOString().slice(0, 10);
return `axonrouter-backup-${date}.ndjson`;
}

async function downloadBackup() {
if (!canDownload) return;
downloadLoading = true;
toast.info('Preparing backup...');
try {
	const blob = await backupApi.downloadBackup({
			password: backupPassword.trim() || undefined,
		});
const url = URL.createObjectURL(blob);
const link = document.createElement('a');
link.href = url;
link.download = backupFilename();
link.click();
URL.revokeObjectURL(url);
toast.success('Backup downloaded');
} catch (err) {
toast.error('Backup failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
} finally {
downloadLoading = false;
}
}

async function restoreBackup() {
if (!restoreFile || !canRestore) return;
restoreLoading = true;
restoreSummary = '';
toast.info('Restoring backup...');
try {
		const result = await backupApi.restoreBackup({
			file: restoreFile,
			password: restorePassword.trim() || undefined,
		});
		restoreSummary = JSON.stringify(result.data ?? result, null, 2);
		toast.success('Backup restored. The gateway is restarting.');
} catch (err) {
toast.error('Restore failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
} finally {
restoreLoading = false;
}
}
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
<div class="space-y-1">
<h1 class="text-display-lg">Backup & Restore.</h1>
	<p class="text-body-sm text-muted-foreground">Export selected gateway data or restore a backup into the running gateway.</p>
</div>

<div class="grid gap-6 xl:grid-cols-2">
<Card.Root class="shadow-card border-border/60">
<Card.Header>
<div class="flex items-start gap-3">
<div class="rounded-xl border border-border bg-muted p-2 text-muted-foreground">
<DownloadIcon class="size-5" />
</div>
<div>
<Card.Title class="text-display-md">Create backup</Card.Title>
					<Card.Description>Downloads a complete backup of all gateway data. Add a password to encrypt the backup payload.</Card.Description>
</div>
</div>
</Card.Header>
			<Card.Content class="space-y-5">
				<div class="rounded-xl border border-border bg-card p-4 text-body-sm text-muted-foreground">
					<p>The backup includes all gateway data: providers, connections, combos, API keys, config, request logs, and cache.</p>
				</div>

				<div class="space-y-2">
<Label for="backup-password" class="text-body-sm-strong">Encryption password (optional)</Label>
<Input id="backup-password" type="password" bind:value={backupPassword} placeholder="Leave blank for plaintext backup" class="h-9" />
<p class="text-caption text-muted-foreground">Use the same password during restore if provided.</p>
</div>

<Button onclick={downloadBackup} disabled={!canDownload} class="w-full text-body-sm rounded-sm cursor-pointer gap-2">
<DownloadIcon class="size-4" />
{downloadLoading ? 'Preparing backup...' : 'Download backup'}
</Button>
</Card.Content>
</Card.Root>

<Card.Root class="shadow-card border-border/60">
<Card.Header>
<div class="flex items-start gap-3">
<div class="rounded-xl border border-border bg-muted p-2 text-muted-foreground">
<UploadIcon class="size-5" />
</div>
<div>
<Card.Title class="text-display-md">Restore backup</Card.Title>
					<Card.Description>Upload an AxonRouter NDJSON backup to replace the data in the running gateway.</Card.Description>
</div>
</div>
</Card.Header>
<Card.Content class="space-y-5">
<div class="space-y-2">
<Label for="restore-file" class="text-body-sm-strong">Backup file</Label>
<Input id="restore-file" type="file" bind:files={restoreFiles} accept=".ndjson,application/x-ndjson,application/json,text/plain" />
{#if restoreFile}
<p class="text-caption text-muted-foreground">Selected: <span class="font-mono">{restoreFile.name}</span></p>
{/if}
</div>

				<div class="space-y-2">
<Label for="restore-password" class="text-body-sm-strong">Decryption password (optional)</Label>
<Input id="restore-password" type="password" bind:value={restorePassword} placeholder="Required only for encrypted backups" class="h-9" />
</div>

<div class="rounded-xl border border-amber-500/30 bg-amber-500/10 p-4 text-body-sm text-amber-200">
<div class="flex gap-2">
<ShieldIcon class="mt-0.5 size-4 shrink-0" />
						<p>Restoring replaces the data in the running gateway. The gateway will restart automatically after a successful restore.</p>
</div>
</div>

<Button onclick={restoreBackup} disabled={!canRestore} class="w-full text-body-sm rounded-sm cursor-pointer gap-2">
<UploadIcon class="size-4" />
{restoreLoading ? 'Restoring backup...' : 'Restore backup'}
</Button>

{#if restoreSummary}
<div class="space-y-2 rounded-xl border border-border bg-card p-4">
<div class="flex items-center gap-2 text-body-sm-strong">
<DatabaseIcon class="size-4" />
Restore result
</div>
<pre class="max-h-56 overflow-auto rounded-sm bg-muted p-3 text-caption-mono text-muted-foreground"><code>{restoreSummary}</code></pre>
</div>
{/if}
</Card.Content>
</Card.Root>
</div>
</div>
