<script lang="ts">
import { onMount } from 'svelte';
import { toast } from 'svelte-sonner';
import { Button } from '$lib/components/ui/button';
import { Badge } from '$lib/components/ui/badge';
import * as Card from '$lib/components/ui/card';
import InfoIcon from '@lucide/svelte/icons/info';
import CodeIcon from '@lucide/svelte/icons/code';
import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
import {
  normalizeVersion,
  isUpdateAvailable,
  parseChangelogForVersion,
  type ChangelogSection,
} from '$lib/about-utils';

const REPO_URL = 'https://github.com/rickicode/AxonRouter-Go';
const RELEASES_URL = `${REPO_URL}/releases/latest`;
const RAW_CHANGELOG_URL = 'https://raw.githubusercontent.com/rickicode/AxonRouter-Go/main/CHANGELOG.md';
const LATEST_RELEASE_API = 'https://api.github.com/repos/rickicode/AxonRouter-Go/releases/latest';

let currentVersion = $state('');
let latestVersion = $state('');
let changelogSections = $state<ChangelogSection[]>([]);
let loading = $state(true);
let changelogLoading = $state(true);
let error = $state('');

const updateAvailable = $derived(
  currentVersion && latestVersion
    ? isUpdateAvailable(currentVersion, latestVersion)
    : false,
);

async function fetchHealth() {
  try {
    const res = await fetch('/api/admin/health');
    if (!res.ok) throw new Error(`Health returned ${res.status}`);
    const data = await res.json();
    currentVersion = typeof data.version === 'string' ? data.version : '';
  } catch (err) {
    error = err instanceof Error ? err.message : 'Failed to load version';
    toast.error('Failed to load version: ' + error);
  }
}

async function fetchLatestVersion() {
  try {
    const res = await fetch(LATEST_RELEASE_API);
    if (!res.ok) throw new Error(`GitHub returned ${res.status}`);
    const data = await res.json();
    const tag = typeof data.tag_name === 'string' ? data.tag_name : '';
    latestVersion = normalizeVersion(tag);
  } catch {
    latestVersion = '';
  }
}

async function fetchChangelog() {
  try {
    const res = await fetch(RAW_CHANGELOG_URL);
    if (!res.ok) throw new Error(`Changelog returned ${res.status}`);
    const markdown = await res.text();
    changelogSections = parseChangelogForVersion(markdown, currentVersion || '0.0.0');
  } catch {
    changelogSections = [];
  } finally {
    changelogLoading = false;
  }
}

onMount(async () => {
  document.title = 'About — AxonRouter';
  await fetchHealth();
  loading = false;
  await Promise.all([fetchLatestVersion(), fetchChangelog()]);
});
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">About.</h1>
    <p class="text-body-sm text-muted-foreground">
      AxonRouter-Go version information, repository links, and release notes.
    </p>
  </div>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md flex items-center gap-2">
        <InfoIcon class="size-5" />
        Project
      </Card.Title>
    </Card.Header>
    <Card.Content class="space-y-4">
      <p class="text-body-sm text-muted-foreground">
        AxonRouter-Go is a universal API proxy built for coding agents. It normalizes provider
        formats, routes requests across providers and combos, tracks usage and quota, and exposes a
        single OpenAI-compatible endpoint for your agent tooling.
      </p>
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md flex items-center gap-2">
        <CodeIcon class="size-5" />
        Repository
      </Card.Title>
      <Card.Description>Source code, issues, and releases.</Card.Description>
    </Card.Header>
    <Card.Content>
      <a
        href={REPO_URL}
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex items-center gap-2 rounded-lg border border-border bg-background px-4 py-3 text-body-sm-strong transition-colors hover:bg-muted"
      >
        <span class="font-mono text-body-sm text-muted-foreground">{REPO_URL}</span>
        <ExternalLinkIcon class="size-4 text-muted-foreground" />
      </a>
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md flex items-center gap-2">
        <ArrowUpCircleIcon class="size-5" />
        Version
      </Card.Title>
      <Card.Description>Current running version and latest upstream release.</Card.Description>
    </Card.Header>
    <Card.Content>
      {#if loading}
        <div class="flex flex-col gap-3">
          <div class="h-6 w-32 animate-pulse rounded-md bg-muted"></div>
          <div class="h-4 w-48 animate-pulse rounded-md bg-muted/60"></div>
        </div>
      {:else if error}
        <p class="text-body-sm text-destructive">{error}</p>
      {:else}
        <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div class="space-y-1">
            <div class="flex items-center gap-3">
              <span class="text-body-sm-strong">Current</span>
              <span class="rounded-md bg-muted px-2 py-0.5 font-mono text-body-sm">
                v{normalizeVersion(currentVersion)}
              </span>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-body-sm-strong">Latest</span>
              {#if latestVersion}
                <span class="rounded-md bg-muted px-2 py-0.5 font-mono text-body-sm">
                  v{latestVersion}
                </span>
              {:else}
                <span class="text-body-sm text-muted-foreground">Unavailable</span>
              {/if}
            </div>
          </div>

          <div class="flex items-center gap-3">
            {#if updateAvailable}
              <Badge variant="destructive" class="rounded-full text-caption">
                Update available
              </Badge>
            {:else if currentVersion && latestVersion}
              <Badge variant="secondary" class="rounded-full text-caption">Up to date</Badge>
            {:else}
              <Badge variant="outline" class="rounded-full text-caption">Unknown</Badge>
            {/if}

            <Button
              href={RELEASES_URL}
              target="_blank"
              rel="noopener noreferrer"
              class="text-body-sm-strong rounded-sm"
              disabled={!latestVersion}
            >
              Upgrade
              <ExternalLinkIcon class="size-4" />
            </Button>
          </div>
        </div>
      {/if}
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Changelog</Card.Title>
      <Card.Description>
        {#if currentVersion}
          Release notes for v{normalizeVersion(currentVersion)}.
        {:else}
          Release notes for the running version.
        {/if}
      </Card.Description>
    </Card.Header>
    <Card.Content>
      {#if changelogLoading}
        <div class="flex flex-col gap-3">
          <div class="h-5 w-40 animate-pulse rounded-md bg-muted"></div>
          <div class="h-4 w-full max-w-md animate-pulse rounded-md bg-muted/60"></div>
          <div class="h-4 w-full max-w-sm animate-pulse rounded-md bg-muted/60"></div>
        </div>
      {:else if changelogSections.length === 0}
        <p class="text-body-sm text-muted-foreground">
          No release notes found for the current version.
        </p>
      {:else}
        <div class="space-y-5">
          {#each changelogSections as section}
            <div>
              <h3 class="text-body-sm-strong mb-2">{section.heading}</h3>
              <ul class="list-disc space-y-1 pl-5 text-body-sm text-muted-foreground">
                {#each section.items as item}
                  <li>{item}</li>
                {/each}
              </ul>
            </div>
          {/each}
        </div>
      {/if}
    </Card.Content>
  </Card.Root>
</div>
