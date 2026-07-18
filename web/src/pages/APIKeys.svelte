<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
import { Input } from '$lib/components/ui/input';
import { Label } from '$lib/components/ui/label';
import { Switch } from '$lib/components/ui/switch';
import { AlertDialog, AlertDialogContent, AlertDialogTitle, AlertDialogDescription, AlertDialogAction, AlertDialogCancel } from '$lib/components/ui/alert-dialog';
  import * as Dialog from '$lib/components/ui/dialog';
  import * as Select from '$lib/components/ui/select';
  import { toast } from 'svelte-sonner';
  import { apiKeysApi } from '$lib/api';
  import { copyToClipboard } from '$lib/copy';
  import { buildExpiryTimestamp, formatExpiry, type ExpirationPreset } from '$lib/api-key-utils';
  import type { APIKeyItem } from '$lib/api';

  let keys = $state<APIKeyItem[]>([]);
  let loading = $state(true);
  let showCreate = $state(false);
  let newName = $state('');
  let newRateLimit = $state('600');
  let newMaxTokensM = $state('');
  let expirationPreset = $state<ExpirationPreset>('never');
  let customDate = $state('');
  let creating = $state(false);
  let createdKey = $state('');
  let createdKeyId = $state('');
let deleteConfirm = $state<{ id: string; name: string } | null>(null);
let showDeleteConfirm = $state(false);
let baseUrl = $state('');

const expirationLabels: Record<ExpirationPreset, string> = {
    never: 'Never',
    '1d': '1 day',
    '7d': '7 days',
    '30d': '30 days',
    '90d': '90 days',
    custom: 'Custom date',
  };

  function setExpirationPreset(v: string) {
    expirationPreset = v as ExpirationPreset;
  }

onMount(() => {
  document.title = 'API Keys — AxonRouter';
  baseUrl = window.location.origin;
  loadKeys();
});

  async function loadKeys() {
    loading = true;
    try {
      const res = await apiKeysApi.list();
      keys = res.data ?? [];
    } catch (err) {
      toast.error('Failed to load API keys');
    } finally {
      loading = false;
    }
  }

  async function handleCreate() {
    creating = true;
    try {
      const m = parseInt(newMaxTokensM) || 0;
      const maxTokens = m > 0 ? m * 1_000_000 : undefined;
      const expiresAt = buildExpiryTimestamp(expirationPreset, customDate);
      const res = await apiKeysApi.create(newName.trim() || undefined, parseInt(newRateLimit) || 600, maxTokens, expiresAt);
      newName = '';
      newMaxTokensM = '';
      expirationPreset = 'never';
      customDate = '';
      createdKey = res.key;
      createdKeyId = res.id;
      toast.success('API key created');
      await loadKeys();
    } catch (err) {
      toast.error('Failed to create key: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      creating = false;
    }
  }

  function handleDelete(id: string, name: string) {
    deleteConfirm = { id, name };
    showDeleteConfirm = true;
  }
  async function confirmDelete() {
    if (!deleteConfirm) return;
    const { id, name } = deleteConfirm;
    deleteConfirm = null;
    showDeleteConfirm = false;
    try {
      await apiKeysApi.delete(id);
      toast.success(`Deleted key: ${name || id.slice(0, 8)}`);
      await loadKeys();
    } catch (err) {
      toast.error('Failed to delete key');
    }
  }


async function handleCopy(key: string) {
	await copyValue(key, 'API key');
}

  async function handleToggle(id: string, current: boolean) {
    try {
      await apiKeysApi.toggle(id, !current);
      toast.success(current ? 'Key disabled' : 'Key enabled');
      await loadKeys();
    } catch (err) {
      toast.error('Failed to toggle key');
    }
  }

async function copyValue(text: string, label = 'Key') {
	await copyToClipboard(text, label);
}

function formatDate(ts: number): string {
    return new Date(ts * 1000).toLocaleDateString();
}

  function formatMaxTokens(tokens: number): string {
    if (!tokens || tokens <= 0) return 'Unlimited';
    return `${Math.round(tokens / 1_000_000)}M`;
  }

  function isExpired(ts?: number): boolean {
    return !!ts && ts <= Date.now() / 1000;
  }
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">API Keys.</h1>
    <p class="text-body-lg text-muted-foreground">
      Manage proxy API keys. When keys exist, all proxy requests require authentication.
    </p>
  </div>

  {#if keys.length === 0 && !loading}
    <Card class="shadow-card">
      <CardContent class="flex flex-col items-center justify-center py-12 gap-3">
        <div class="size-12 rounded-full bg-muted/50 flex items-center justify-center">
          <svg class="size-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" />
          </svg>
        </div>
        <div class="text-center">
          <p class="text-sm font-medium text-muted-foreground">No API keys configured</p>
          <p class="text-xs text-muted-foreground/70 mt-0.5">Proxy is currently open. Add a key to require authentication.</p>
        </div>
        <Button onclick={() => showCreate = true} size="sm" class="text-body-sm rounded-sm mt-1">
          Create API key
        </Button>
      </CardContent>
    </Card>
  {:else}
    <div class="flex items-center justify-between">
      <p class="text-caption-mono text-muted-foreground">{keys.length} key{keys.length !== 1 ? 's' : ''}</p>
      <Button onclick={() => { showCreate = true; createdKey = ''; }} size="sm" class="text-body-sm rounded-sm">
        Create key
      </Button>
    </div>

    <Card class="shadow-card overflow-hidden">
      <CardContent class="p-0">
        <div class="overflow-x-auto">
          <table class="w-full text-left border-collapse">
            <thead>
              <tr class="border-b border-border bg-muted/30">
                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Name</th>
                                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Rate Limit</th>
              <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Status</th>
              <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Created</th>
              <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Expires</th>
              <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-32"></th>
              </tr>
            </thead>
	<tbody class="divide-y divide-border">
		{#each keys as key}
			<tr class="transition-colors hover:bg-accent/20">
				<td class="py-3 px-4">
					<div class="text-body-sm font-medium">{key.name || '—'}</div>
					<div class="flex items-center gap-2 mt-1">
						<code class="font-mono text-xs text-muted-foreground break-all">{key.key || '—'}</code>
						{#if key.key}
						<Button variant="outline" size="sm" class="h-6 px-1.5 py-0.5 text-caption-mono cursor-pointer" onclick={() => handleCopy(key.key)}>Copy</Button>
						{/if}
					</div>
				</td>
				<td class="py-3 px-4 text-body-sm text-muted-foreground">{key.rate_limit_per_min}/min · {key.max_tokens > 0 ? formatMaxTokens(key.max_tokens) : 'Unlimited'}</td>
<td class="py-3 px-4">
              <div class="flex justify-center">
                <Switch checked={key.is_active} onCheckedChange={() => handleToggle(key.id, key.is_active)} aria-label={key.is_active ? 'Disable key' : 'Enable key'} />
              </div>
            </td>
              <td class="py-3 px-4 text-body-sm text-muted-foreground">{formatDate(key.created_at)}</td>
              <td class="py-3 px-4">
                {#if key.expires_at}
                  {@const expired = isExpired(key.expires_at)}
                  <span class="text-body-sm {expired ? 'text-destructive font-medium' : 'text-muted-foreground'}" title={new Date(key.expires_at * 1000).toLocaleString()}>
                    {formatExpiry(key.expires_at)}
                  </span>
                {:else}
                  <span class="text-body-sm text-muted-foreground">Never</span>
                {/if}
              </td>
              <td class="py-3 px-4">
              <Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm text-destructive hover:text-destructive" onclick={() => handleDelete(key.id, key.name)}>
                Del
              </Button>
            </td>
			</tr>
		{/each}
	</tbody>
          </table>
        </div>
      </CardContent>
    </Card>
{/if}

{#if !loading}
<Card class="shadow-card">
  <CardHeader>
    <CardTitle class="text-display-md">Proxy API docs.</CardTitle>
  </CardHeader>
  <CardContent class="space-y-6">
    <div class="space-y-2">
      <h3 class="text-body-sm-strong">Base URL</h3>
      <div class="flex items-center gap-2 flex-wrap">
        <code class="font-mono text-sm text-foreground bg-muted px-2 py-1 rounded-sm">{baseUrl}/v1</code>
        <Button variant="outline" size="sm" class="h-7 px-2 text-caption-mono cursor-pointer rounded-sm" onclick={() => copyValue(baseUrl + '/v1', 'Base URL')}>
          Copy
        </Button>
      </div>
    </div>

    <div class="space-y-2">
      <h3 class="text-body-sm-strong">Authentication</h3>
      <p class="text-body-sm text-muted-foreground">Send your API key in the Authorization header on every <code>/v1/*</code> request.</p>
      <pre class="bg-muted p-3 rounded-sm text-caption-mono overflow-x-auto"><code>Authorization: Bearer &lt;YOUR_API_KEY&gt;</code></pre>
    </div>

    <div class="space-y-2">
      <h3 class="text-body-sm-strong">Supported endpoints</h3>
      <ul class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-body-sm text-muted-foreground">
        <li><code class="text-foreground">POST /v1/chat/completions</code> — Chat</li>
        <li><code class="text-foreground">GET /v1/models</code> — Model list</li>
        <li><code class="text-foreground">POST /v1/messages</code> — Claude Messages</li>
        <li><code class="text-foreground">POST /v1/responses</code> — Responses API</li>
        <li><code class="text-foreground">POST /v1/embeddings</code> — Embeddings</li>
        <li><code class="text-foreground">POST /v1/audio/speech</code> — Text-to-speech</li>
        <li><code class="text-foreground">POST /v1/audio/transcriptions</code> — Speech-to-text</li>
        <li><code class="text-foreground">POST /v1/images/generations</code> — Images</li>
        <li><code class="text-foreground">POST /v1/video/generations</code> — Video</li>
        <li><code class="text-foreground">POST /v1/unified</code> — Unified gateway</li>
        <li><code class="text-foreground">POST /v1/messages/count_tokens</code> — Token count</li>
      </ul>
    </div>

    <div class="space-y-2">
      <h3 class="text-body-sm-strong">Example</h3>
      <pre class="bg-muted p-4 rounded-sm text-caption-mono overflow-x-auto"><code>curl -H "Authorization: Bearer &lt;YOUR_API_KEY&gt;" \
  -X POST {baseUrl}/v1/chat/completions \
  -d '&#123;"model":"openai/gpt-4o","messages":[&#123;"role":"user","content":"Hello"&#125;]&#125;'</code></pre>
      <p class="text-caption text-muted-foreground">Model IDs must include the provider prefix, e.g. <code class="text-foreground">openai/gpt-4o</code>, <code class="text-foreground">claude/claude-sonnet-4</code>, or <code class="text-foreground">cx/gpt-5.4</code>.</p>
    </div>
  </CardContent>
</Card>
{/if}

<!-- Create dialog -->
<Dialog.Root bind:open={showCreate}>
  <Dialog.Content class="sm:max-w-md">
    {#if createdKey}
          <h2 class="text-lg font-semibold mb-2">Key created</h2>
          <p class="text-sm text-muted-foreground mb-4">Copy this key now — it won't be shown again.</p>
          <div class="rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3 mb-4">
            <p class="font-mono text-sm break-all select-all">{createdKey}</p>
          </div>
          <div class="flex gap-2">
            <Button onclick={() => copyValue(createdKey, 'Key')} class="flex-1 gap-2 text-sm">
              <svg class="size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>
              Copy key
            </Button>
            <Button variant="outline" onclick={() => { showCreate = false; createdKey = ''; }} class="text-sm">Done</Button>
          </div>
        {:else}
          <h2 class="text-lg font-semibold mb-4">Create API key</h2>
	<div class="flex flex-col gap-4">
		<div class="flex flex-col gap-1.5">
			<Label class="text-sm font-medium">Name (optional)</Label>
			<Input bind:value={newName} placeholder="My API key" class="h-9 text-sm" />
		</div>
		<div class="flex flex-col gap-1.5">
			<Label class="text-sm font-medium">Rate limit (per minute)</Label>
			<Input bind:value={newRateLimit} type="number" min="1" class="h-9 text-sm" />
		</div>
            <div class="flex flex-col gap-1.5">
              <Label class="text-sm font-medium">Max tokens (M)</Label>
              <Input bind:value={newMaxTokensM} type="number" min="1" placeholder="Unlimited" class="h-9 text-sm" />
              <p class="text-xs text-muted-foreground">Leave empty for unlimited. Min 1M (1 = 1,000,000 tokens).</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label class="text-sm font-medium">Expiration</Label>
              <Select.Root type="single" value={expirationPreset} onValueChange={setExpirationPreset}>
                <Select.Trigger class="w-full h-9 text-body-sm rounded-sm">
                  {expirationLabels[expirationPreset]}
                </Select.Trigger>
                <Select.Content>
                  {#each Object.entries(expirationLabels) as [value, label]}
                    <Select.Item {value} class="text-body-sm">{label}</Select.Item>
                  {/each}
                </Select.Content>
              </Select.Root>
              {#if expirationPreset === 'custom'}
                <Input type="date" bind:value={customDate} min={new Date().toISOString().split('T')[0]} class="h-9 text-sm" />
                <p class="text-xs text-muted-foreground">Expires at 23:59:59 UTC on the selected date.</p>
              {/if}
            </div>
          </div>
          <div class="flex gap-2 mt-6">
	<Button onclick={handleCreate} disabled={creating} class="flex-1 text-sm">
		{creating ? 'Creating...' : 'Create'}
	</Button>
	<Button variant="outline" onclick={() => { showCreate = false; createdKey = ''; }} class="text-sm">Cancel</Button>
</div>

{/if}
</Dialog.Content>
</Dialog.Root>

  <AlertDialog bind:open={showDeleteConfirm}>
    <AlertDialogContent>
      <AlertDialogTitle>Delete API key</AlertDialogTitle>
      <AlertDialogDescription>
        Are you sure you want to delete <strong>{deleteConfirm?.name || deleteConfirm?.id.slice(0, 8)}</strong>? This action cannot be undone.
      </AlertDialogDescription>
      <div class="flex gap-2 justify-end mt-4">
        <AlertDialogCancel onclick={() => { deleteConfirm = null; showDeleteConfirm = false; }}>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive" onclick={confirmDelete}>Delete</AlertDialogAction>
      </div>
    </AlertDialogContent>
  </AlertDialog>
</div>
