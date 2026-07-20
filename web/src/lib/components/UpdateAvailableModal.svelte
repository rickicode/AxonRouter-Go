<script lang="ts">
import * as Dialog from '$lib/components/ui/dialog';
import { Button } from '$lib/components/ui/button';
import { Badge } from '$lib/components/ui/badge';
import { currentPath } from '$lib/router';
import {
  healthUpdateAvailable,
  healthCurrentVersion,
  healthLatestVersion,
} from '$lib/health';
import { getToken } from '$lib/auth';
import {
  normalizeVersion,
  parseChangelogForVersion,
  type ChangelogSection,
} from '$lib/about-utils';
import { isUpdateModalDismissed, dismissUpdateModal } from '$lib/update-modal';
import { toast } from 'svelte-sonner';
import RocketIcon from '@lucide/svelte/icons/rocket';
import FileTextIcon from '@lucide/svelte/icons/file-text';
import XIcon from '@lucide/svelte/icons/x';
import Loader2Icon from '@lucide/svelte/icons/loader-2';
import CopyIcon from '@lucide/svelte/icons/copy';
import CheckIcon from '@lucide/svelte/icons/check';

let dismissed = $state(isUpdateModalDismissed());
let open = $state(false);
let changelogSections = $state<ChangelogSection[]>([]);
let changelogLoading = $state(false);
let changelogError = $state('');
let upgrading = $state(false);
let upgradeJustCompleted = $state(false);
let restartCommand = $state('');
let restartHint = $state('');
let copiedCommand = $state(false);
let upgradeLogs = $state<string[]>([]);
let restartInitiated = $state(false);
let restarting = $state(false);

const UPGRADE_TIMEOUT_MS = 70000;
const RESTART_TIMEOUT_MS = 10000;

const currentNorm = $derived(normalizeVersion($healthCurrentVersion));
const latestNorm = $derived(normalizeVersion($healthLatestVersion));
const shouldShow = $derived(
  $healthUpdateAvailable && $currentPath !== '/about' && !dismissed
);

$effect(() => {
	if (shouldShow) {
		open = true;
		void fetchChangelog();
	}
});

async function fetchChangelog() {
	changelogLoading = true;
	changelogError = '';
	try {
		const headers: Record<string, string> = {};
		const token = getToken();
		if (token) headers['Authorization'] = 'Bearer ' + token;
		const res = await fetch('/api/admin/changelog', { headers });
		if (!res.ok) {
			const data = await res.json().catch(() => ({}));
			throw new Error(data.error || `Changelog returned ${res.status}`);
		}
		const data = await res.json();
		const markdown = typeof data.markdown === 'string' ? data.markdown : '';
		changelogSections = parseChangelogForVersion(markdown, $healthLatestVersion || '0.0.0');
	} catch (err) {
		changelogSections = [];
		changelogError = err instanceof Error ? err.message : 'Failed to load release notes';
	} finally {
		changelogLoading = false;
	}
}

function handleOpenChange(next: boolean) {
	open = next;
	if (!next) {
		dismissUpdateModal();
		dismissed = true;
	}
}

async function handleUpgrade() {
	if (upgrading || upgradeJustCompleted) return;
	upgrading = true;

	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), UPGRADE_TIMEOUT_MS);

	try {
		const headers: Record<string, string> = {};
		const token = getToken();
		if (token) headers['Authorization'] = 'Bearer ' + token;

		const res = await fetch('/api/admin/upgrade', {
			method: 'POST',
			headers,
			signal: controller.signal,
		});
		const data = await res.json().catch(() => ({}));

		if (!res.ok) {
			throw new Error(data.error || `Upgrade returned ${res.status}`);
		}

		const path = typeof data.path === 'string' ? data.path : '';
		restartCommand = typeof data.restart_command === 'string' ? data.restart_command : '';
		restartHint = typeof data.restart_hint === 'string' ? data.restart_hint : '';
		const logs = Array.isArray(data.logs) ? data.logs : [];
		upgradeLogs = logs;
		restartInitiated = false;
		upgradeJustCompleted = true;
		toast.success(path ? `Upgrade saved to ${path}` : 'Upgrade completed');
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Upgrade failed';
		toast.error('Upgrade failed: ' + message);
	} finally {
		clearTimeout(timeout);
		upgrading = false;
	}
}

async function copyRestartCommand() {
	if (!restartCommand) return;
	try {
		await navigator.clipboard.writeText(restartCommand);
		copiedCommand = true;
		toast.success('Restart command copied');
		setTimeout(() => { copiedCommand = false; }, 2000);
	} catch {
		toast.error('Copy failed');
	}
}

async function handleRestart() {
	if (restarting || restartInitiated) return;
	restarting = true;

	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), RESTART_TIMEOUT_MS);

	try {
		const headers: Record<string, string> = {};
		const token = getToken();
		if (token) headers['Authorization'] = 'Bearer ' + token;

		const res = await fetch('/api/admin/restart', {
			method: 'POST',
			headers,
			signal: controller.signal,
		});
		const data = await res.json().catch(() => ({}));

		if (!res.ok) {
			restartCommand = typeof data.restart_command === 'string' ? data.restart_command : restartCommand;
			throw new Error(data.error || `Restart returned ${res.status}`);
		}

		restartInitiated = true;
		toast.success('Restarting service...', { description: 'The gateway is restarting in the background.' });
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Restart failed';
		toast.error('Restart failed: ' + message, {
			description: restartCommand ? 'Run the command below to restart manually.' : undefined,
		});
	} finally {
		clearTimeout(timeout);
		restarting = false;
	}
}
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
	<Dialog.Content class="sm:max-w-[480px]">
		<Dialog.Header>
			<div class="flex items-center gap-3">
				<span class="flex size-10 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-400">
					<RocketIcon class="size-5" />
				</span>
				<div>
					<Dialog.Title class="text-display-md">Update available</Dialog.Title>
					<Dialog.Description class="text-body-sm text-muted-foreground">
						A new release of AxonRouter is ready to install.
					</Dialog.Description>
				</div>
			</div>
		</Dialog.Header>

		<div class="py-2 space-y-4">
			{#if upgradeJustCompleted && restartCommand}
				<div class="space-y-3 rounded-lg border border-emerald-500/20 bg-card p-4">
					<div class="flex items-center gap-2">
						<RocketIcon class="size-4 text-emerald-400" />
						<h3 class="text-body-sm-strong">Upgrade complete</h3>
					</div>

					{#if upgradeLogs.length > 0}
						<div class="space-y-2">
							<p class="text-caption text-muted-foreground uppercase">Upgrade logs</p>
							<div class="max-h-40 overflow-y-auto rounded-lg bg-muted border border-border p-3 space-y-1">
								{#each upgradeLogs as log}
									<p class="text-caption-mono text-foreground">{log}</p>
								{/each}
							</div>
						</div>
					{/if}

					<div class="flex items-center gap-2 p-3 rounded-lg bg-muted border border-border overflow-x-auto">
						<code class="text-body-sm font-mono whitespace-nowrap flex-1">{restartCommand}</code>
						<Button
							variant="outline"
							size="sm"
							class="rounded-sm cursor-pointer gap-1.5 flex-shrink-0"
							onclick={copyRestartCommand}
							disabled={copiedCommand}
						>
							{#if copiedCommand}
								<CheckIcon class="size-3.5" />
								<span class="text-body-sm">Copied</span>
							{:else}
								<CopyIcon class="size-3.5" />
								<span class="text-body-sm">Copy</span>
							{/if}
						</Button>
					</div>
					{#if restartHint}
						<p class="text-caption text-muted-foreground">{restartHint}</p>
					{/if}

					{#if !restartInitiated}
						<div class="flex flex-col gap-3 pt-2 border-t border-border">
							<p class="text-body-sm">Restart the axonrouter service now?</p>
							<div class="flex items-center gap-2">
								<Button
									size="sm"
									class="rounded-sm cursor-pointer gap-1.5"
									onclick={handleRestart}
									disabled={restarting}
								>
									{#if restarting}
										<Loader2Icon class="size-3.5 animate-spin" />
									{/if}
									<span class="text-body-sm">Restart now</span>
								</Button>
								<Button
									variant="outline"
									size="sm"
									class="rounded-sm cursor-pointer"
									onclick={() => { upgradeJustCompleted = false; }}
								>
									Later
								</Button>
							</div>
						</div>
					{:else}
						<div class="flex items-center gap-2 text-emerald-400 pt-2 border-t border-border">
							<CheckIcon class="size-4" />
							<p class="text-body-sm-strong">Restart initiated</p>
						</div>
					{/if}
				</div>
			{:else}
				<div class="flex items-center gap-2 flex-wrap">
					<Badge variant="secondary" class="rounded-sm text-caption-mono">
						v{currentNorm || '—'} → v{latestNorm || '—'}
					</Badge>
					<Badge variant="outline" class="rounded-sm text-caption text-emerald-400 border-emerald-400/30">
						Newer release
					</Badge>
				</div>

				{#if changelogLoading}
					<div class="space-y-2">
						<div class="h-4 w-32 animate-pulse rounded bg-muted"></div>
						<div class="h-3 w-full animate-pulse rounded bg-muted/60"></div>
						<div class="h-3 w-5/6 animate-pulse rounded bg-muted/60"></div>
					</div>
				{:else if changelogError}
					<p class="text-body-sm text-destructive">{changelogError}</p>
				{:else if changelogSections.length === 0}
					<p class="text-body-sm text-muted-foreground">No release notes found for v{latestNorm}.</p>
				{:else}
					<div class="space-y-4 max-h-[240px] overflow-y-auto pr-1">
						{#each changelogSections as section}
							<div>
								<h4 class="text-caption-mono uppercase text-muted-foreground mb-1.5">{section.heading}</h4>
								<ul class="space-y-1.5">
									{#each section.items.slice(0, 5) as item}
										<li class="flex gap-2 text-body-sm text-muted-foreground">
											<span class="mt-1.5 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-primary"></span>
											<span class="text-balance">{item}</span>
										</li>
									{/each}
									{#if section.items.length > 5}
										<li class="text-caption text-muted-foreground/70 pl-3.5">
											+{section.items.length - 5} more on the About page.
										</li>
									{/if}
								</ul>
							</div>
						{/each}
					</div>
				{/if}
			{/if}
		</div>

		<Dialog.Footer class="flex-col gap-2 sm:flex-col">
			{#if upgradeJustCompleted && restartCommand}
				<Button
					class="h-11 w-full gap-2 rounded-sm cursor-pointer"
					onclick={handleRestart}
					disabled={restarting || restartInitiated}
				>
					{#if restarting}
						<Loader2Icon class="size-4 animate-spin" />
					{:else}
						<RocketIcon class="size-4" />
					{/if}
					<span>{restarting ? 'Restarting…' : 'Restart now'}</span>
				</Button>
				<Button
					variant="outline"
					class="h-11 w-full gap-2 rounded-sm cursor-pointer"
					onclick={() => { upgradeJustCompleted = false; }}
				>
					<XIcon class="size-4" />
					Later
				</Button>
			{:else}
				<Button
					class="h-11 w-full gap-2 rounded-sm cursor-pointer"
					onclick={handleUpgrade}
					disabled={upgrading}
				>
					{#if upgrading}
						<Loader2Icon class="size-4 animate-spin" />
					{:else}
						<RocketIcon class="size-4" />
					{/if}
					<span>{upgrading ? 'Upgrading…' : 'Upgrade now'}</span>
				</Button>
				<div class="flex gap-2 w-full">
					<Button
						variant="outline"
						class="h-11 flex-1 gap-2 rounded-sm cursor-pointer"
						onclick={() => { window.open('https://github.com/rickicode/AxonRouter-Go/releases', '_blank', 'noopener,noreferrer'); }}
					>
						<FileTextIcon class="size-4" />
						Releases
					</Button>
					<Button
						variant="outline"
						class="h-11 flex-1 gap-2 rounded-sm cursor-pointer"
						onclick={() => handleOpenChange(false)}
					>
						<XIcon class="size-4" />
						Later
					</Button>
				</div>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
