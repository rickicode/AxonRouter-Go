<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Badge } from '$lib/components/ui/badge';
  import { AlertDialog, AlertDialogContent, AlertDialogTitle, AlertDialogDescription, AlertDialogAction, AlertDialogCancel } from '$lib/components/ui/alert-dialog';
import * as Dialog from '$lib/components/ui/dialog';
  import { toast } from 'svelte-sonner';
  import { apiKeysApi } from '$lib/api';
  import type { APIKeyItem } from '$lib/api';

  let keys = $state<APIKeyItem[]>([]);
  let loading = $state(true);
  let showCreate = $state(false);
  let newName = $state('');
  let newRateLimit = $state('600');
  let newMaxTokensM = $state('');
  let creating = $state(false);
  let createdKey = $state('');
  let createdKeyId = $state('');
  let deleteConfirm = $state<{ id: string; name: string } | null>(null);
  let showDeleteConfirm = $state(false);

  onMount(() => {
    document.title = 'API Keys — AxonRouter';
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
		const res = await apiKeysApi.create(newName.trim() || undefined, parseInt(newRateLimit) || 600, maxTokens);
		newName = '';
		newMaxTokensM = '';
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
    try {
		await copyValue(key, 'API key');
	} catch (err) {
		toast.error('Failed to copy API key');
    }
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
	try {
		if (navigator.clipboard && window.isSecureContext) {
			await navigator.clipboard.writeText(text);
		} else {
			// Fallback for HTTP: temporary textarea + execCommand
			const ta = document.createElement('textarea');
			ta.value = text;
			ta.style.position = 'fixed';
			ta.style.left = '-9999px';
			document.body.appendChild(ta);
			ta.select();
			document.execCommand('copy');
			document.body.removeChild(ta);
		}
		toast.success(`${label} copied to clipboard`);
	} catch {
		toast.error('Copy failed — select and copy manually');
	}
}

function formatDate(ts: number): string {
    return new Date(ts * 1000).toLocaleDateString();
}

function formatMaxTokens(tokens: number): string {
    if (!tokens || tokens <= 0) return 'Unlimited';
    return `${Math.round(tokens / 1_000_000)}M`;
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
                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Key</th>
                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Rate Limit</th>
                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Status</th>
                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4">Created</th>
                <th class="text-caption-mono text-muted-foreground uppercase font-semibold py-3 px-4 w-32"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              {#each keys as key}
                <tr class="transition-colors hover:bg-accent/20">
                  <td class="py-3 px-4 text-body-sm font-medium">{key.name || '—'}</td>
                  <td class="py-3 px-4 font-mono text-xs break-all max-w-xs">{key.key || '—'}</td>
 		<td class="py-3 px-4 text-body-sm text-muted-foreground">{key.rate_limit_per_min}/min{key.max_tokens > 0 ? ` · ${formatMaxTokens(key.max_tokens)}` : ''}</td>
                  <td class="py-3 px-4">
                    <Badge variant={key.is_active ? 'default' : 'secondary'} class="text-caption-mono rounded-sm">
                      {key.is_active ? 'Active' : 'Disabled'}
                    </Badge>
                  </td>
                  <td class="py-3 px-4 text-body-sm text-muted-foreground">{formatDate(key.created_at)}</td>
                  <td class="py-3 px-4">
                    <div class="flex gap-1">
				<Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm" onclick={() => handleCopy(key.key)}>
									Copy
								</Button>
								<Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm" onclick={() => handleToggle(key.id, key.is_active)}>
									{key.is_active ? 'Disable' : 'Enable'}
								</Button>
								<Button variant="ghost" size="sm" class="text-body-sm h-7 px-2 rounded-sm text-destructive hover:text-destructive" onclick={() => handleDelete(key.id, key.name)}>
									Del
								</Button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
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
