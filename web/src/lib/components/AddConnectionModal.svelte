<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Textarea } from '$lib/components/ui/textarea';
  import { Badge } from '$lib/components/ui/badge';
  import { connectionsApi, providersApi } from '$lib/api';
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

  type Step = 'form' | 'oauth-waiting' | 'done' | 'error';
  type Mode = 'single' | 'bulk';

  let step = $state<Step>('form');
  let mode = $state<Mode>('single');
  let connectionName = $state('');
  let apiKey = $state('');
  let showKey = $state(false);
  let bulkText = $state('');
  let submitting = $state(false);
  let errorMsg = $state('');
  let oauthPolling = $state(false);
  let createdConnId = $state('');
  let oauthUrl = $state('');
  let oauthStatusText = $state('Waiting for browser authorization...');
  let callbackUrl = $state('');
  let submittingCallback = $state(false);
  let validating = $state(false);
  let validationResult = $state<'success' | 'failed' | null>(null);
  let oauthPopup: Window | null = null;

  const authType = $derived(meta?.authType ?? 'apikey');
  const isOAuth = $derived(authType === 'oauth');
  const isNoAuth = $derived(authType === 'none');
  const isApiKey = $derived(authType === 'apikey' || authType === 'custom');
  const supportsBulk = $derived(isApiKey);

  function reset() {
    step = 'form';
    mode = 'single';
    connectionName = '';
    apiKey = '';
    showKey = false;
    bulkText = '';
    errorMsg = '';
    validating = false;
    validationResult = null;
    submitting = false;
    oauthPolling = false;
    createdConnId = '';
    oauthUrl = '';
    oauthStatusText = 'Waiting for browser authorization...';
    callbackUrl = '';
    submittingCallback = false;
    oauthPopup?.close();
    oauthPopup = null;
  }

  function handleOpenChange(isOpen: boolean) {
    if (!isOpen) reset();
    open = isOpen;
  }

  function defaultName(index?: number): string {
    const base = meta?.displayName ?? providerId;
    return typeof index === 'number' ? `${base} ${index}` : `${base} connection`;
  }

  function parseBulkConnections() {
    return bulkText
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line, index) => {
        const match = line.match(/^([^,:\t]+)[,:\t](.+)$/);
        if (!match) return { name: defaultName(index + 1), api_key: line };
        const name = match[1].trim() || defaultName(index + 1);
        const key = match[2].trim();
        return { name, api_key: key };
      })
      .filter((conn) => conn.api_key.length > 0);
  }

  async function copyOAuthUrl() {
    if (!oauthUrl) return;
    try {
      await navigator.clipboard.writeText(oauthUrl);
      toast.success('OAuth URL copied');
    } catch {
      toast.error('Copy failed — select the URL manually');
    }
  }

  async function submitOAuthCallbackUrl() {
    if (!createdConnId || !callbackUrl.trim()) {
      toast.error('Paste the callback URL first');
      return;
    }
    submittingCallback = true;
    errorMsg = '';
    try {
      await connectionsApi.submitOAuthCallback(createdConnId, callbackUrl.trim());
      toast.success('Callback submitted');
      oauthStatusText = 'Callback submitted. Finalizing tokens...';
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Callback submit failed';
      toast.error(errorMsg);
    } finally {
      submittingCallback = false;
    }
  }

  async function handleValidate() {
    if (!apiKey.trim()) return;
    validating = true;
    validationResult = null;
    try {
      const res = await providersApi.validateKey(providerId, apiKey.trim());
      validationResult = res.valid ? 'success' : 'failed';
    } catch {
      validationResult = 'failed';
    } finally {
      validating = false;
    }
  }

  async function handleApiKeySubmit() {
    errorMsg = '';
    submitting = true;
    try {
      if (mode === 'bulk') {
        const connections = parseBulkConnections();
        if (connections.length === 0) throw new Error('Paste at least one API key');
        const result = await connectionsApi.bulkCreate(providerId, { connections });
        toast.success(`Added ${result.created}/${result.total} connections`);
        step = 'done';
        onCreated?.();
        return;
      }

      // Auto-validate before saving (like AxonRouter TS)
      if (apiKey.trim()) {
        try {
          validating = true;
          validationResult = null;
          const res = await providersApi.validateKey(providerId, apiKey.trim());
          validationResult = res.valid ? 'success' : 'failed';
        } catch {
          validationResult = 'failed';
        } finally {
          validating = false;
        }
      }

      const name = connectionName.trim() || defaultName();
      const data = {
        name,
        auth_type: 'api_key' as const,
        ...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
      };
      await connectionsApi.create(providerId, data);
      toast.success(`Connection added: ${name}`);
      step = 'done';
      onCreated?.();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to add connection';
      toast.error(errorMsg);
      step = 'error';
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
      toast.success(`Connection added: ${name}`);
      step = 'done';
      onCreated?.();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to add connection';
      toast.error(errorMsg);
      step = 'error';
    } finally {
      submitting = false;
    }
  }

  async function handleOAuthSubmit() {
    errorMsg = '';
    submitting = true;
    oauthUrl = '';
    oauthStatusText = 'Starting OAuth login...';

    // Open synchronously from the click handler so browsers do not classify it as a blocked async popup.
    oauthPopup = window.open('about:blank', '_blank');
    if (oauthPopup) {
      oauthPopup.document.title = 'AxonRouter OAuth';
      oauthPopup.document.body.innerHTML = '<p style="font-family:system-ui;padding:24px">Preparing OAuth login...</p>';
    }

    try {
      // Auto-generate name like OmniRoute — backend will update with email after OAuth completes
      const name = `OAuth ${meta?.displayName ?? providerId}`;
      const conn = await connectionsApi.create(providerId, {
        name,
        auth_type: 'oauth',
      });
      createdConnId = conn.id;

      const oauthRes = await connectionsApi.initiateOAuth(conn.id);
      oauthUrl = oauthRes.auth_url;
      step = 'oauth-waiting';
      oauthPolling = true;
      oauthStatusText = 'Browser opened. Complete authorization there.';

      if (oauthPopup) {
        oauthPopup.location.href = oauthRes.auth_url;
      } else {
        toast.error('Popup blocked — use the Open OAuth URL button');
      }

      toast.info(`OAuth started for ${meta?.displayName ?? providerId}`);
      pollOAuthStatus(conn.id);
    } catch (err) {
      oauthPopup?.close();
      oauthPopup = null;
      errorMsg = err instanceof Error ? err.message : 'Failed to start OAuth';
      toast.error(errorMsg);
      step = 'error';
    } finally {
      submitting = false;
    }
  }

  async function pollOAuthStatus(connId: string) {
    const maxAttempts = 150; // 5 minutes at 2s intervals, same timeout window as CLIProxyAPI.
    for (let i = 0; i < maxAttempts; i += 1) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      if (!oauthPolling) return;
      try {
        const status = await connectionsApi.oauthStatus(connId);
        if (status.connected) {
          oauthPolling = false;
          oauthPopup?.close();
          oauthPopup = null;
          const accountName = status.name || (meta?.displayName ?? providerId);
          oauthStatusText = `Connected as ${accountName}`;
          toast.success(`OAuth connected: ${accountName}`);
          step = 'done';
          onCreated?.();
          return;
        }
      } catch {
        // Ignore transient status errors while the local callback server is waiting.
      }
    }

    oauthPolling = false;
    oauthStatusText = 'OAuth timed out. The connection is not eligible until authentication succeeds.';
    toast.error('OAuth timed out after 5 minutes');
    step = 'error';
    onCreated?.();
  }

  function cancelOAuth() {
    oauthPolling = false;
    oauthPopup?.close();
    oauthPopup = null;
    toast.info('OAuth flow cancelled');
    handleOpenChange(false);
  }

  function handleSubmit() {
    if (isOAuth) return handleOAuthSubmit();
    if (isNoAuth) return handleNoAuthSubmit();
    return handleApiKeySubmit();
  }
  // Auto-start OAuth when modal opens for OAuth providers (matches AxonRouter TS behavior)
  $effect(() => {
    if (open && isOAuth && step === 'form' && !submitting && !oauthPolling) {
      // Use setTimeout to avoid calling during render
      setTimeout(() => handleOAuthSubmit(), 50);
    }
  });

</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
  <Dialog.Content class="sm:max-w-[560px]">
    {#if step === 'form'}
      <Dialog.Header>
        <div class="flex items-start gap-3">
          {#if meta}
            <div
              class="size-11 shrink-0 overflow-hidden rounded-lg border border-border/50 flex items-center justify-center"
              style="background-color: {(meta.color ?? '#888')}15"
            >
              <ProviderIcon {meta} size={44} />
            </div>
          {/if}
          <div class="min-w-0 space-y-1">
            <Dialog.Title class="text-lg font-semibold">
              {isOAuth ? 'Connect OAuth account' : isNoAuth ? 'Add no-auth connection' : 'Add API key'}
            </Dialog.Title>
            <Dialog.Description class="text-sm text-muted-foreground">
              {meta?.displayName ?? providerId} · {isOAuth ? 'browser login' : isNoAuth ? 'no credential required' : 'single or bulk credential'}
            </Dialog.Description>
            <div class="flex flex-wrap gap-1.5 pt-1">
              <Badge variant="outline" class="rounded-full text-caption-mono">{meta?.prefix ?? `${providerId}/`}</Badge>
              <Badge variant="outline" class="rounded-full text-caption-mono">{meta?.format ?? 'openai'}</Badge>
              <Badge variant="secondary" class="rounded-full text-caption-mono">
                {isOAuth ? 'OAuth' : isNoAuth ? 'No auth' : 'API key'}
              </Badge>
            </div>
          </div>
        </div>
      </Dialog.Header>

      <div class="flex flex-col gap-4 py-2">
        {#if supportsBulk}
          <div class="grid grid-cols-2 gap-2 rounded-lg border border-border/50 bg-muted/20 p-1">
            <button
              type="button"
              class="rounded-md px-3 py-2 text-sm transition-colors {mode === 'single' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
              onclick={() => mode = 'single'}
            >
              Single key
            </button>
            <button
              type="button"
              class="rounded-md px-3 py-2 text-sm transition-colors {mode === 'bulk' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
              onclick={() => mode = 'bulk'}
            >
              Bulk import
            </button>
          </div>
        {/if}

        {#if !isOAuth && mode === 'single'}
          <div class="flex flex-col gap-1.5">
            <Label class="text-sm font-medium">Connection name</Label>
            <Input bind:value={connectionName} placeholder={defaultName()} class="h-9 text-sm" />
          </div>
        {/if}

        {#if isApiKey && mode === 'single'}
          <div class="flex flex-col gap-1.5">
            <Label class="text-sm font-medium">
              API key
              {#if meta?.authHint}
                <span class="font-normal text-muted-foreground">({meta.authHint})</span>
              {/if}
            </Label>
            <div class="flex gap-2">
              <div class="relative flex-1">
                <Input
                  bind:value={apiKey}
                  type={showKey ? 'text' : 'password'}
                  placeholder={meta?.apiHint ?? 'sk-...'}
                  class="h-9 pr-10 font-mono text-sm"
                  autocomplete="off"
                  spellcheck={false}
                />
                <button
                  type="button"
                  class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                  onclick={() => showKey = !showKey}
                  tabindex={-1}
                >
                  <span class="material-symbols-outlined text-base">{showKey ? 'visibility_off' : 'visibility'}</span>
                </button>
              </div>
              <Button variant="secondary" class="h-9 text-sm" disabled={!apiKey.trim() || validating || submitting} onclick={handleValidate}>
                {validating ? 'Checking...' : 'Check'}
              </Button>
            </div>
            {#if validationResult}
              <Badge variant={validationResult === 'success' ? 'default' : 'destructive'} class="w-fit text-caption-mono">
                {validationResult === 'success' ? 'Valid' : 'Invalid'}
              </Badge>
            {/if}
            <p class="text-[11px] text-muted-foreground">
              {meta?.apiHint ?? 'Stored as one AxonRouter connection and used for routing.'}
            </p>
          </div>
        {:else if isApiKey && mode === 'bulk'}
          <div class="flex flex-col gap-1.5">
            <Label class="text-sm font-medium">API keys</Label>
            <Textarea
              bind:value={bulkText}
              class="min-h-36 font-mono text-xs"
              placeholder={`sk-...\nmain: sk-...\nbackup, sk-...`}
              spellcheck={false}
            />
            {#if bulkText.trim()}
              <p class="text-[11px] text-emerald-400">
                {parseBulkConnections().length} key{parseBulkConnections().length !== 1 ? 's' : ''} detected
              </p>
            {/if}
            <p class="text-[11px] text-muted-foreground">
              One key per line. Optional format: <span class="font-mono">name: key</span> or <span class="font-mono">name, key</span>.
            </p>
          </div>
        {/if}

        {#if isOAuth}
          <div class="rounded-lg border border-border/50 bg-muted/30 p-3 text-sm text-muted-foreground">
            <p>A browser tab opens automatically. Complete login there — this modal waits up to 5 minutes for the callback.</p>
          </div>
        {/if}

        {#if isNoAuth}
          <div class="rounded-lg border border-border/50 bg-muted/30 p-3 text-sm text-muted-foreground">
            This provider does not require a credential. Add a named ready connection for routing.
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
            {isOAuth ? 'Starting OAuth...' : mode === 'bulk' ? 'Importing...' : 'Adding...'}
          {:else if isOAuth}
            Connect
          {:else if mode === 'bulk'}
            Import keys
          {:else}
            Add connection
          {/if}
        </Button>
      </Dialog.Footer>
    {:else if step === 'oauth-waiting'}
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">Waiting for authentication</Dialog.Title>
        <Dialog.Description class="text-sm text-muted-foreground">
          Complete login in the browser window. Keep this modal open.
        </Dialog.Description>
      </Dialog.Header>

      <div class="flex flex-col items-center gap-4 py-6">
        <div class="relative size-16">
          <div class="absolute inset-0 rounded-full border-2 border-muted"></div>
          <div class="absolute inset-0 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
          <div class="absolute inset-0 flex items-center justify-center">
            <span class="material-symbols-outlined text-primary">lock_open</span>
          </div>
        </div>
        <div class="space-y-2 text-center">
          <p class="text-sm font-medium">{oauthStatusText}</p>
        </div>
        {#if oauthUrl}
          <div class="w-full space-y-3 rounded-lg border border-border/50 bg-muted/20 p-3">
            <div>
              <p class="mb-2 text-xs text-muted-foreground">If the browser did not open, use this URL.</p>
              <div class="flex gap-2">
                <Button variant="outline" class="text-sm" onclick={() => window.open(oauthUrl, '_blank')}>Open OAuth URL</Button>
                <Button variant="outline" class="text-sm" onclick={copyOAuthUrl}>Copy URL</Button>
              </div>
            </div>
            <div class="space-y-1.5 border-t border-border/50 pt-3">
              <Label class="text-xs text-muted-foreground">Remote/browser fallback callback URL</Label>
              <div class="flex gap-2">
                <Input
                  bind:value={callbackUrl}
                  class="h-9 min-w-0 flex-1 font-mono text-xs"
                  placeholder="http://localhost:1455/auth/callback?code=...&state=..."
                  autocomplete="off"
                  spellcheck={false}
                />
                <Button variant="outline" class="text-sm" disabled={submittingCallback} onclick={submitOAuthCallbackUrl}>
                  {submittingCallback ? 'Submitting...' : 'Submit'}
                </Button>
              </div>
              <p class="text-[11px] text-muted-foreground">
                If OAuth lands on a localhost error page, paste that full URL here. AxonRouter will forward it to the backend callback server.
              </p>
            </div>
          </div>
        {/if}
      </div>

      <Dialog.Footer>
        <Button variant="outline" onclick={cancelOAuth} class="text-sm">Cancel</Button>
      </Dialog.Footer>
    {:else if step === 'error'}
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">Connection not ready</Dialog.Title>
        <Dialog.Description class="text-sm text-muted-foreground">
          {errorMsg || oauthStatusText}
        </Dialog.Description>
      </Dialog.Header>
      <Dialog.Footer>
        <Button variant="outline" onclick={() => { step = 'form'; errorMsg = ''; }} class="text-sm">Back</Button>
        <Button onclick={() => handleOpenChange(false)} class="text-sm">Close</Button>
      </Dialog.Footer>
    {:else}
      <Dialog.Header>
        <Dialog.Title class="text-lg font-semibold">
          {isOAuth ? 'Connected' : 'Connection added'}
        </Dialog.Title>
      </Dialog.Header>

      <div class="flex flex-col items-center gap-3 py-4">
        <div class="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-500/10">
          <svg class="h-6 w-6 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <p class="text-sm text-muted-foreground">{oauthStatusText || 'Connection is ready to use.'}</p>
      </div>

      <Dialog.Footer>
        <Button onclick={() => { reset(); handleOpenChange(false); }} class="text-sm">Done</Button>
      </Dialog.Footer>
    {/if}
  </Dialog.Content>
</Dialog.Root>
