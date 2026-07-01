<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { connectionsApi } from '$lib/api';
  import { toast } from 'svelte-sonner';
  import ProviderIcon from '$lib/components/ProviderIcon.svelte';
  import type { ProviderMeta } from '$lib/provider-catalog';

  let {
    open = $bindable(false),
    providerId,
    meta,
    onCreated,
  }: {
    open: boolean;
    providerId: string;
    meta: ProviderMeta | undefined;
    onCreated?: () => void;
  } = $props();

  let step = $state<'form' | 'oauth-waiting' | 'done'>('form');
  let connectionName = $state('');
  let apiKey = $state('');
  let submitting = $state(false);
  let errorMsg = $state('');
  let oauthPolling = $state(false);
  let createdConnId = $state('');

  const authType = $derived(meta?.authType ?? 'apikey');
  const isOAuth = $derived(authType === 'oauth');
  const isNoAuth = $derived(authType === 'none');
  const isApiKey = $derived(authType === 'apikey' || authType === 'custom');

  function reset() {
    step = 'form';
    connectionName = '';
    apiKey = '';
    errorMsg = '';
    submitting = false;
    oauthPolling = false;
    createdConnId = '';
  }

  function handleOpenChange(isOpen: boolean) {
    if (!isOpen) reset();
    open = isOpen;
  }

  function defaultName(): string {
    const base = meta?.displayName ?? providerId;
    return `${base} connection`;
  }

  async function handleApiKeySubmit() {
    errorMsg = '';
    submitting = true;
    try {
      const name = connectionName.trim() || defaultName();
      const data: Record<string, unknown> = {
        name,
        auth_type: 'api_key',
      };
      if (apiKey.trim()) {
        data.api_key = apiKey.trim();
      }
      await connectionsApi.create(providerId, data);
      toast.success(`Connection "${name}" added`);
      step = 'done';
      onCreated?.();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to add connection';
      toast.error(errorMsg);
    } finally {
      submitting = false;
    }
  }

  async function handleNoAuthSubmit() {
    errorMsg = '';
    submitting = true;
    try {
      const name = connectionName.trim() || defaultName();
      await connectionsApi.create(providerId, {
        name,
        auth_type: 'none',
      });
      toast.success(`Connection "${name}" added`);
      step = 'done';
      onCreated?.();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to add connection';
      toast.error(errorMsg);
    } finally {
      submitting = false;
    }
  }

  async function handleOAuthSubmit() {
    errorMsg = '';
    submitting = true;
    try {
      // Step 1: Create connection with oauth auth type
      const name = connectionName.trim() || defaultName();
      const conn = await connectionsApi.create(providerId, {
        name,
        auth_type: 'oauth',
      });
      createdConnId = conn.id;

      // Step 2: Initiate OAuth flow
      const oauthRes = await connectionsApi.initiateOAuth(conn.id);

      // Step 3: Open auth URL (same window to avoid popup blocker)
      window.location.href = oauthRes.auth_url;

      step = 'oauth-waiting';
      oauthPolling = true;

      // Step 4: Poll for OAuth completion
      pollOAuthStatus(conn.id);
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to start OAuth';
      toast.error(errorMsg);
    } finally {
      submitting = false;
    }
  }

  async function pollOAuthStatus(connId: string) {
    const maxAttempts = 60; // 5 minutes at 5s intervals
    for (let i = 0; i < maxAttempts; i++) {
      await new Promise(r => setTimeout(r, 5000));
      if (!oauthPolling) break;
      try {
        const status = await connectionsApi.oauthStatus(connId);
        if (status.connected) {
          oauthPolling = false;
          toast.success('OAuth connected successfully');
          step = 'done';
          onCreated?.();
          return;
        }
      } catch {
        // ignore poll errors
      }
    }
    oauthPolling = false;
    toast.error('OAuth timed out. The connection was created — re-authenticate from the connection page.');
    step = 'done';
    onCreated?.();
  }

  function handleSubmit() {
    if (isOAuth) return handleOAuthSubmit();
    if (isNoAuth) return handleNoAuthSubmit();
    return handleApiKeySubmit();
  }
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
  <Dialog.Content class="sm:max-w-[480px]">
    {#if step === 'form'}
      <Dialog.Header>
        <div class="flex items-center gap-3">
          {#if meta}
            <div
              class="size-10 rounded-lg flex items-center justify-center shrink-0 overflow-hidden"
              style="background-color: {(meta.color ?? '#888')}15"
            >
              <ProviderIcon {meta} size={40} />
            </div>
          {/if}
          <div>
            <Dialog.Title class="text-lg font-semibold">
              Add connection
            </Dialog.Title>
            <Dialog.Description class="text-sm text-muted-foreground">
              {meta?.displayName ?? providerId}
              {#if isOAuth}
                · OAuth authentication
              {:else if isNoAuth}
                · No authentication required
              {:else}
                · API key authentication
              {/if}
            </Dialog.Description>
          </div>
        </div>
      </Dialog.Header>

      <div class="flex flex-col gap-4 py-2">
        <!-- Connection name -->
        <div class="flex flex-col gap-1.5">
          <Label class="text-sm font-medium">Connection name</Label>
          <Input
            bind:value={connectionName}
            placeholder={defaultName()}
            class="h-9 text-sm"
          />
        </div>

        {#if isApiKey}
          <!-- API Key input -->
          <div class="flex flex-col gap-1.5">
            <Label class="text-sm font-medium">
              API key
              {#if meta?.authHint}
                <span class="text-muted-foreground font-normal">({meta.authHint})</span>
              {/if}
            </Label>
            <Input
              bind:value={apiKey}
              type="password"
              placeholder="sk-..."
              class="h-9 text-sm font-mono"
            />
            {#if meta?.apiHint}
              <p class="text-[11px] text-muted-foreground">{meta.apiHint}</p>
            {/if}
          </div>
        {/if}

        {#if isOAuth}
          <div class="rounded-lg border border-border/50 bg-muted/30 p-3">
            <p class="text-sm text-muted-foreground">
              A browser window will open for authentication. After authorizing,
              the connection will be configured automatically.
            </p>
          </div>
        {/if}

        {#if isNoAuth}
          <div class="rounded-lg border border-border/50 bg-muted/30 p-3">
            <p class="text-sm text-muted-foreground">
              This provider doesn't require authentication. Just give your connection a name.
            </p>
          </div>
        {/if}

        {#if meta?.hasFree && meta?.freeNote}
          <div class="rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3">
            <p class="text-sm text-emerald-400">{meta.freeNote}</p>
          </div>
        {/if}

        {#if errorMsg}
          <p class="rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-sm text-destructive">{errorMsg}</p>
        {/if}
      </div>

      <Dialog.Footer>
        <Button variant="outline" onclick={() => handleOpenChange(false)} class="text-sm">Cancel</Button>
        <Button onclick={handleSubmit} disabled={submitting} class="text-sm">
          {#if submitting}
            {isOAuth ? 'Starting OAuth...' : 'Adding...'}
          {:else if isOAuth}
            Connect with OAuth
          {:else if isNoAuth}
            Add connection
          {:else}
            Add connection
          {/if}
        </Button>
      </Dialog.Footer>

    {:else if step === 'oauth-waiting'}
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">Waiting for authorization</Dialog.Title>
        <Dialog.Description class="text-sm text-muted-foreground">
          Complete the authentication in the browser window that opened.
        </Dialog.Description>
      </Dialog.Header>

      <div class="flex flex-col items-center gap-4 py-6">
        <div class="relative size-16">
          <div class="absolute inset-0 rounded-full border-2 border-muted"></div>
          <div class="absolute inset-0 rounded-full border-2 border-primary border-t-transparent animate-spin"></div>
          <div class="absolute inset-0 flex items-center justify-center">
            <svg class="size-6 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
          </div>
        </div>
        <div class="text-center">
          <p class="text-sm font-medium">Waiting for OAuth callback...</p>
          <p class="text-xs text-muted-foreground mt-1">This will auto-complete once you authorize in the browser.</p>
        </div>
      </div>

      <Dialog.Footer>
        <Button variant="outline" onclick={() => { oauthPolling = false; handleOpenChange(false); }} class="text-sm">
          Cancel
        </Button>
      </Dialog.Footer>

    {:else}
      <!-- Success -->
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">Connection added</Dialog.Title>
      </Dialog.Header>

      <div class="flex flex-col items-center gap-3 py-4">
        <div class="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-500/10">
          <svg class="h-6 w-6 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <p class="text-sm text-muted-foreground">Connection is ready to use.</p>
      </div>

      <Dialog.Footer>
        <Button onclick={() => { reset(); handleOpenChange(false); }} class="text-sm">Done</Button>
      </Dialog.Footer>
    {/if}
  </Dialog.Content>
</Dialog.Root>
