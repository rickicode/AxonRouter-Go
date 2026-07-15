<script lang="ts">
import { onMount } from 'svelte';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Switch } from '$lib/components/ui/switch';
import { tlsApi, type TLSConfig, type TLSCheckDNSResult } from '$lib/api';
import { toast } from 'svelte-sonner';
import GlobeLockIcon from '@lucide/svelte/icons/globe-lock';
import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
import CopyIcon from '@lucide/svelte/icons/copy';
import { copyToClipboard } from '$lib/copy';
import CheckCircleIcon from '@lucide/svelte/icons/check-circle';
import XCircleIcon from '@lucide/svelte/icons/x-circle';
import GlobeIcon from '@lucide/svelte/icons/globe';
import SaveIcon from '@lucide/svelte/icons/save';
import Loader2Icon from '@lucide/svelte/icons/loader-2';

let config = $state<TLSConfig>({
  enabled: false,
  domain: '',
  email: '',
  acceptTOS: false,
  staging: false,
  certCache: 'certs',
});
let publicIP = $state('');
let loading = $state(true);
let saving = $state(false);
let dnsLoading = $state(false);
let dnsResult = $state<TLSCheckDNSResult | null>(null);

onMount(async () => {
  await Promise.all([loadConfig(), loadIP()]);
});

async function loadConfig() {
  try {
    const res = await tlsApi.get();
    if (res.data) config = res.data;
  } catch (err) {
    toast.error('Failed to load HTTPS config: ' + (err instanceof Error ? err.message : 'Unknown'));
  }
}

async function loadIP() {
  try {
    const res = await tlsApi.publicIp();
    publicIP = res.data.ip;
  } catch (err) {
    publicIP = '';
    toast.error('Failed to detect public IP: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    loading = false;
  }
}

async function copyIP() {
	if (!publicIP) return;
	await copyToClipboard(publicIP, 'Public IP');
}

async function handleCheckDns() {
  if (!config.domain) {
    toast.error('Enter a domain first');
    return;
  }
  dnsLoading = true;
  dnsResult = null;
  try {
    const res = await tlsApi.checkDns(config.domain);
    dnsResult = res.data;
  } catch (err) {
    toast.error('DNS check failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    dnsLoading = false;
  }
}

async function handleSave() {
  if (!config.domain || !config.email) {
    toast.error('Domain and email are required');
    return;
  }
  if (config.enabled && !config.acceptTOS) {
    toast.error("Accept the Let's Encrypt terms to enable HTTPS");
    return;
  }
  saving = true;
  try {
    await tlsApi.save({
      enabled: config.enabled,
      domain: config.domain,
      email: config.email,
      acceptTOS: config.acceptTOS,
      staging: config.staging,
      certCache: config.certCache || 'certs',
    });
    toast.success('HTTPS config saved');
  } catch (err) {
    toast.error('Save failed: ' + (err instanceof Error ? err.message : 'Unknown'));
  } finally {
    saving = false;
  }
}
</script>

<Card class="shadow-card border-border/60">
  <CardHeader class="pb-4">
    <div class="flex items-center gap-3">
      <span class="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
        <GlobeLockIcon class="size-5" />
      </span>
      <div>
        <CardTitle class="text-body-md-strong">HTTPS on port 443</CardTitle>
        <CardDescription class="text-body-sm">
          Automatic certificates via Let's Encrypt. Requires a public A record pointing to this server.
        </CardDescription>
      </div>
    </div>
  </CardHeader>

  <CardContent class="space-y-6">
    {#if loading}
      <div class="flex flex-col gap-3">
        {#each Array(4) as _}
          <div class="h-12 bg-muted animate-pulse rounded-md"></div>
        {/each}
      </div>
    {:else}
    {#if config.active}
      <div class="flex items-start gap-3 rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-body-sm text-emerald-700 dark:text-emerald-400">
        <CheckCircleIcon class="size-5 shrink-0" />
        <div>
          <p class="font-medium">HTTPS is active on port 443.</p>
          {#if config.certDir}
            <p class="text-caption text-muted-foreground">Certificates are cached in <span class="font-mono">{config.certDir}</span>.</p>
          {/if}
        </div>
      </div>
    {:else if config.enabled}
      <div class="flex items-start gap-3 rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-body-sm text-yellow-700 dark:text-yellow-400">
        <AlertTriangleIcon class="size-5 shrink-0" />
        <p>Restart AxonRouter to activate HTTPS on port 443.</p>
      </div>
    {/if}

      {#if publicIP}
        <div class="space-y-2">
          <Label class="text-body-sm-strong">Public IP</Label>
          <div class="flex items-center gap-2 rounded-md border border-border bg-muted p-2">
            <span class="text-code font-mono text-body-sm">{publicIP}</span>
            <Button onclick={copyIP} variant="ghost" size="sm" class="ml-auto h-7 gap-1 text-body-sm">
              <CopyIcon class="size-4" />
              Copy
            </Button>
          </div>
          <p class="text-caption text-muted-foreground">
            Create or update an A record for <span class="font-mono">{config.domain || 'your domain'}</span> pointing to this IP.
          </p>
        </div>
      {/if}

      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div class="space-y-2">
          <Label for="https-domain" class="text-body-sm-strong">Domain</Label>
          <Input id="https-domain" bind:value={config.domain} placeholder="api.example.com" class="h-10 text-body-sm" />
        </div>
        <div class="space-y-2">
          <Label for="https-email" class="text-body-sm-strong">Email</Label>
          <Input id="https-email" type="email" bind:value={config.email} placeholder="admin@example.com" class="h-10 text-body-sm" />
        </div>
      </div>

      <div class="space-y-3">
        <div class="flex items-center justify-between rounded-lg border border-border bg-card p-3">
          <div>
            <Label for="https-enabled" class="text-body-sm-strong">Enable HTTPS</Label>
            <p class="text-caption text-muted-foreground">Listen on port 443 after restart.</p>
          </div>
          <Switch id="https-enabled" bind:checked={config.enabled} />
        </div>

        <div class="flex items-center justify-between rounded-lg border border-border bg-card p-3">
          <div>
            <Label for="https-tos" class="text-body-sm-strong">Accept Let's Encrypt ToS</Label>
            <p class="text-caption text-muted-foreground">Required to request certificates.</p>
          </div>
          <Switch id="https-tos" bind:checked={config.acceptTOS} />
        </div>

        <div class="flex items-center justify-between rounded-lg border border-border bg-card p-3">
          <div>
            <Label for="https-staging" class="text-body-sm-strong">Use Let's Encrypt Staging</Label>
            <p class="text-caption text-muted-foreground">Test issuance without rate limits.</p>
          </div>
          <Switch id="https-staging" bind:checked={config.staging} />
        </div>
      </div>

      {#if dnsResult}
        <div class="rounded-lg border {dnsResult.matches ? 'border-emerald-500/30 bg-emerald-500/10' : 'border-destructive/30 bg-destructive/10'} p-3 space-y-1">
          <div class="flex items-center gap-2 text-body-sm-strong {dnsResult.matches ? 'text-emerald-700 dark:text-emerald-400' : 'text-destructive'}">
            {#if dnsResult.matches}
              <CheckCircleIcon class="size-4" />
              DNS matches
            {:else}
              <XCircleIcon class="size-4" />
              DNS does not match
            {/if}
          </div>
          <p class="text-caption text-muted-foreground">
            Resolved: <span class="font-mono">{dnsResult.resolvedIP || '—'}</span>
            · Expected: <span class="font-mono">{dnsResult.publicIP}</span>
          </p>
        </div>
      {/if}

      <div class="flex flex-wrap gap-2">
        <Button onclick={handleCheckDns} disabled={dnsLoading || !config.domain} variant="outline" size="sm" class="text-body-sm rounded-sm gap-2">
          {#if dnsLoading}
            <Loader2Icon class="size-4 animate-spin" />
          {:else}
            <GlobeIcon class="size-4" />
          {/if}
          Check DNS
        </Button>

        <Button onclick={handleSave} disabled={saving || !config.domain || !config.email} size="sm" class="text-body-sm rounded-sm gap-2">
          {#if saving}
            <Loader2Icon class="size-4 animate-spin" />
          {:else}
            <SaveIcon class="size-4" />
          {/if}
          Save HTTPS config
        </Button>
      </div>
    {/if}
  </CardContent>
</Card>
