<script lang="ts">
import { onMount } from 'svelte';
import { get } from 'svelte/store';
import { toast } from 'svelte-sonner';
import { Button } from '$lib/components/ui/button';
import { Badge } from '$lib/components/ui/badge';
import * as Card from '$lib/components/ui/card';
import CodeIcon from '@lucide/svelte/icons/code';
import BookOpenIcon from '@lucide/svelte/icons/book-open';
import RocketIcon from '@lucide/svelte/icons/rocket';
import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
import Loader2Icon from '@lucide/svelte/icons/loader-2';
import CopyIcon from '@lucide/svelte/icons/copy';
import CheckIcon from '@lucide/svelte/icons/check';
import { getToken } from '$lib/auth';
import {
  healthCurrentVersion,
  healthLatestVersion,
  healthUpdateAvailable,
} from '$lib/health';
import {
  normalizeVersion,
  parseChangelogForVersion,
  type ChangelogSection,
} from '$lib/about-utils';

const REPO_URL = 'https://github.com/rickicode/AxonRouter-Go';
const UPGRADE_TIMEOUT_MS = 70000;

let currentVersion = $state(get(healthCurrentVersion));
let latestVersion = $state(get(healthLatestVersion));
let updateAvailable = $state(get(healthUpdateAvailable));
let changelogSections = $state<ChangelogSection[]>([]);
let loading = $state(true);
let changelogLoading = $state(true);
let changelogError = $state('');
let upgrading = $state(false);
let upgradeJustCompleted = $state(false);
let restartCommand = $state('');
let restartHint = $state('');
let copiedCommand = $state(false);
let upgradeLogs = $state<string[]>([]);
let restartInitiated = $state(false);
let restarting = $state(false);
const RESTART_TIMEOUT_MS = 10000;

const normalizedCurrent = $derived(currentVersion ? normalizeVersion(currentVersion) : '');
const normalizedLatest = $derived(latestVersion ? normalizeVersion(latestVersion) : '');

$effect(() => {
	currentVersion = $healthCurrentVersion;
	latestVersion = $healthLatestVersion;
	updateAvailable = $healthUpdateAvailable;
	upgradeJustCompleted = false;
});

async function fetchChangelog() {
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
    changelogSections = parseChangelogForVersion(markdown, $healthCurrentVersion || '0.0.0');
  } catch (err) {
    changelogSections = [];
    changelogError = err instanceof Error ? err.message : 'Failed to load release notes';
  } finally {
    changelogLoading = false;
  }
}

async function handleUpgrade() {
  if (upgrading) return;
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

let checking = $state(false);

async function checkForUpdates() {
  if (checking) return;
  checking = true;
  try {
    const headers: Record<string, string> = {};
    const token = getToken();
    if (token) headers['Authorization'] = 'Bearer ' + token;

    const res = await fetch('/api/admin/upgrade/check', { headers });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || `Check update returned ${res.status}`);
    }
    const data = await res.json();
    currentVersion = typeof data.version === 'string' ? data.version : currentVersion;
    latestVersion = typeof data.latest_version === 'string' ? data.latest_version : '';
    updateAvailable = data.update_available === true;

    healthCurrentVersion.set(currentVersion);
    healthLatestVersion.set(latestVersion);
    healthUpdateAvailable.set(updateAvailable);

    await fetchChangelog();

    if (updateAvailable) {
      toast.info(`Update available: v${normalizedLatest}`, { description: 'Click Upgrade now to install the latest release.' });
    } else if (currentVersion && latestVersion) {
      toast.success('Up to date', { description: `v${normalizedCurrent} is the latest release.` });
    } else {
      toast.error('Unable to check for updates');
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Check update failed';
    toast.error('Check update failed: ' + message);
  } finally {
    checking = false;
  }
}

onMount(() => {
  document.title = 'About — AxonRouter';
  void (async () => {
    if (!latestVersion) {
      await checkForUpdates();
    } else {
      await fetchChangelog();
    }
    loading = false;
  })();
});
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Header -->
  <div class="space-y-1">
    <h1 class="text-display-lg">About.</h1>
    <p class="text-body-sm text-muted-foreground">Version, release notes, and project links.</p>
  </div>

  <!-- Hero -->
  <Card.Root class="relative overflow-hidden shadow-elevated border-border/40">
    <div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-violet/10 pointer-events-none"></div>
    <Card.Content class="relative p-6 md:p-8">
      {#if loading}
        <div class="flex flex-col gap-4 max-w-2xl">
          <div class="h-6 w-40 animate-pulse rounded-md bg-muted"></div>
          <div class="h-10 w-72 animate-pulse rounded-md bg-muted"></div>
          <div class="h-4 w-full max-w-lg animate-pulse rounded-md bg-muted/60"></div>
        </div>
      {:else}
        <div class="flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
          <div class="space-y-4 max-w-2xl">
            <div class="flex flex-wrap items-center gap-2">
              <Badge variant="secondary" class="rounded-sm text-caption-mono">AxonRouter-Go</Badge>
              {#if updateAvailable}
                <Badge variant="destructive" class="rounded-sm text-caption">Update available</Badge>
              {:else if currentVersion && latestVersion}
                <Badge variant="outline" class="rounded-sm text-caption text-emerald-400 border-emerald-400/30">Up to date</Badge>
              {/if}
            </div>
            <h2 class="text-display-md text-balance">Universal API proxy built for coding agents.</h2>
            <p class="text-body-sm text-muted-foreground text-balance max-w-xl">
              Normalizes provider formats, routes requests across providers and combos, tracks usage and quota, and exposes a single OpenAI-compatible endpoint for your agent tooling.
            </p>
          </div>
          <div class="flex flex-col gap-3 md:min-w-[180px]">
            <Button
              variant="outline"
              class="gap-2 rounded-sm cursor-pointer"
              onclick={() => window.open(REPO_URL, '_blank', 'noopener,noreferrer')}
            >
              <CodeIcon class="size-4" /> Source code
            </Button>
            <Button
              class="gap-2 rounded-sm cursor-pointer"
              onclick={handleUpgrade}
              disabled={!updateAvailable || upgrading || upgradeJustCompleted}
            >
              {#if upgrading}
                <Loader2Icon class="size-4 animate-spin" />
              {:else}
                <RocketIcon class="size-4" />
              {/if}
              <span>{upgrading ? 'Upgrading…' : updateAvailable ? 'Upgrade now' : 'Up to date'}</span>
            </Button>
          </div>
        </div>
      {/if}
	</Card.Content>
</Card.Root>

{#if upgradeJustCompleted && restartCommand}
	<Card.Root class="shadow-card border-emerald-500/20">
		<Card.Content class="p-4 space-y-3">
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
				<div class="flex flex-col sm:flex-row sm:items-center gap-3 pt-1 border-t border-border">
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
				<div class="flex items-center gap-2 text-emerald-400 pt-1 border-t border-border">
					<CheckIcon class="size-4" />
					<p class="text-body-sm-strong">Restart initiated</p>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
{/if}

{#if !loading}
    <!-- Version facts -->
    <section class="grid grid-cols-1 md:grid-cols-3 gap-4">
      <Card.Root class="shadow-card">
        <Card.Content class="p-4">
          <p class="text-caption text-muted-foreground uppercase">Current version</p>
          <p class="text-body-md-strong font-mono mt-1">{normalizedCurrent ? 'v' + normalizedCurrent : '—'}</p>
        </Card.Content>
      </Card.Root>
      <Card.Root class={updateAvailable ? 'shadow-card border-emerald-500/50 bg-emerald-500/5' : 'shadow-card'}>
        <Card.Content class="p-4">
          <div class="flex items-center justify-between">
            <p class="text-caption text-muted-foreground uppercase">Latest release</p>
            {#if updateAvailable}
              <Badge variant="outline" class="rounded-sm text-caption text-emerald-400 border-emerald-400/30">Newer</Badge>
            {/if}
          </div>
          <p class="text-body-md-strong font-mono mt-1">{normalizedLatest ? 'v' + normalizedLatest : '—'}</p>
          {#if updateAvailable}
            <p class="text-caption text-emerald-400 mt-1">An upgrade is available.</p>
          {/if}
        </Card.Content>
      </Card.Root>
      <Card.Root class="shadow-card">
        <Card.Content class="p-4">
          <p class="text-caption text-muted-foreground uppercase">Repository</p>
          <a
            href={REPO_URL}
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-primary hover:text-primary/80 text-body-sm-strong mt-1"
          >
            rickicode/AxonRouter-Go <ExternalLinkIcon class="size-3" />
          </a>
        </Card.Content>
      </Card.Root>
    </section>
  {/if}

  <!-- Release notes -->
  <Card.Root class="shadow-card">
    <Card.Header>
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="space-y-1">
          <Card.Title class="text-display-md flex items-center gap-2">
            <BookOpenIcon class="size-5" /> Release notes
          </Card.Title>
          <Card.Description>
            {currentVersion ? `Changelog for v${normalizedCurrent}.` : 'Release notes for the running version.'}
          </Card.Description>
        </div>
        <div class="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            class="rounded-sm cursor-pointer w-fit gap-1.5"
            onclick={checkForUpdates}
            disabled={checking}
          >
            {#if checking}
              <Loader2Icon class="size-3.5 animate-spin" />
            {:else}
              <RefreshCwIcon class="size-3.5" />
            {/if}
            Check for updates
          </Button>
          <Button
            variant="outline"
            size="sm"
            class="rounded-sm cursor-pointer w-fit"
            onclick={() => window.open(`${REPO_URL}/releases`, '_blank', 'noopener,noreferrer')}
          >
            View releases <ExternalLinkIcon class="size-3 ml-1" />
          </Button>
        </div>
      </div>
    </Card.Header>
    <Card.Content>
      {#if changelogLoading}
        <div class="flex flex-col gap-3">
          <div class="h-5 w-40 animate-pulse rounded-md bg-muted"></div>
          <div class="h-4 w-full max-w-md animate-pulse rounded-md bg-muted/60"></div>
          <div class="h-4 w-full max-w-sm animate-pulse rounded-md bg-muted/60"></div>
        </div>
      {:else if changelogError}
        <div class="rounded-lg bg-muted/30 p-4 border border-border/50 space-y-2">
          <p class="text-body-sm text-destructive">{changelogError}</p>
          <a
            href={`${REPO_URL}/releases`}
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-primary hover:text-primary/80 text-body-sm"
          >
            Open on GitHub <ExternalLinkIcon class="size-3" />
          </a>
        </div>
      {:else if changelogSections.length === 0}
        <div class="rounded-lg bg-muted/30 p-4 border border-border/50 space-y-2">
          <p class="text-body-sm text-muted-foreground">No release notes found for the current version.</p>
          <a
            href={`${REPO_URL}/releases`}
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex items-center gap-1 text-primary hover:text-primary/80 text-body-sm"
          >
            Open on GitHub <ExternalLinkIcon class="size-3" />
          </a>
        </div>
      {:else}
        <div class="space-y-6">
          {#each changelogSections as section}
            <div>
              <Badge variant="secondary" class="rounded-sm text-caption mb-2">{section.heading}</Badge>
              <ul class="space-y-2">
                {#each section.items as item}
                  <li class="flex gap-3 text-body-sm text-muted-foreground">
                    <span class="mt-1.5 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-primary"></span>
                    <span class="text-balance">{item}</span>
                  </li>
                {/each}
              </ul>
            </div>
          {/each}
        </div>
      {/if}
    </Card.Content>
  </Card.Root>
</div>
