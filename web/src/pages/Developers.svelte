<script lang="ts">
  import { onMount } from 'svelte';
  import { toast } from 'svelte-sonner';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import * as Card from '$lib/components/ui/card';
  import { developersApi } from '$lib/api';
  import CopyIcon from '@lucide/svelte/icons/copy';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import CodeIcon from '@lucide/svelte/icons/code';

  type MasterKeyInfo = {
    key: string;
    prefix: string;
    base_url: string;
    created_at: number;
  };

  let keyInfo: MasterKeyInfo | null = $state(null);
  let loading = $state(false);

  async function loadKey() {
    loading = true;
    try {
      const res = await developersApi.getMasterKey();
      keyInfo = res.data;
    } catch (err: any) {
      toast.error(err.message || 'Failed to load master key');
    } finally {
      loading = false;
    }
  }

  async function regenerate() {
    loading = true;
    try {
      const res = await developersApi.regenerateMasterKey();
      keyInfo = res.data;
      toast.success('Master key regenerated');
    } catch (err: any) {
      toast.error(err.message || 'Failed to regenerate master key');
    } finally {
      loading = false;
    }
  }

  function copy(text: string, label: string) {
    navigator.clipboard.writeText(text).then(() => {
      toast.success(`${label} copied to clipboard`);
    }).catch(() => {
      toast.error('Copy failed');
    });
  }

  const endpoints = [
    { method: 'GET', path: '/admin/api/v1/providers' },
    { method: 'GET', path: '/admin/api/v1/api-keys' },
    { method: 'POST', path: '/admin/api/v1/api-keys' },
    { method: 'GET', path: '/admin/api/v1/logs' },
    { method: 'GET', path: '/admin/api/v1/model-pricing' },
    { method: 'GET', path: '/admin/api/v1/settings' },
  ];

  onMount(loadKey);
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Developers.</h1>
    <p class="text-body-sm text-muted-foreground">
      Programmatic admin API access. Keep the master key secret; it grants full gateway control.
    </p>
  </div>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md flex items-center gap-2">
        <CodeIcon class="size-5" />
        Master API Key
      </Card.Title>
      <Card.Description>
        Use this key in the <code>Authorization: Bearer &lt;key&gt;</code> header for all <code>/admin/api/v1</code> requests.
      </Card.Description>
    </Card.Header>
    <Card.Content class="space-y-6">
      {#if keyInfo}
        <div class="space-y-2">
          <label for="master-key" class="text-caption text-muted-foreground">Key</label>
          <div class="flex gap-2">
            <Input id="master-key" value={keyInfo.key} readonly class="font-mono text-body-sm bg-muted" />
            <Button variant="outline" size="icon" onclick={() => copy(keyInfo!.key, 'Master key')} aria-label="Copy master key">
              <CopyIcon class="size-4" />
            </Button>
          </div>
          <p class="text-caption text-muted-foreground">
            Prefix: <span class="font-mono">{keyInfo.prefix}</span> · Created: <span>{new Date(keyInfo.created_at * 1000).toLocaleString()}</span>
          </p>
        </div>

        <div class="space-y-2">
          <label for="base-url" class="text-caption text-muted-foreground">Base URL</label>
          <div class="flex gap-2">
            <Input id="base-url" value={keyInfo.base_url} readonly class="font-mono text-body-sm bg-muted" />
            <Button variant="outline" size="icon" onclick={() => copy(keyInfo!.base_url, 'Base URL')} aria-label="Copy base URL">
              <CopyIcon class="size-4" />
            </Button>
          </div>
        </div>

        <div class="flex gap-2">
          <Button onclick={regenerate} disabled={loading}>
            <RefreshCwIcon class="size-4 mr-2" />
            Regenerate
          </Button>
        </div>
      {:else}
        <p class="text-body-sm text-muted-foreground">Loading master key...</p>
      {/if}
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Endpoints</Card.Title>
      <Card.Description>A short reference for the most common programmatic admin calls.</Card.Description>
    </Card.Header>
    <Card.Content>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-body-sm">
          <thead>
            <tr class="border-b border-border text-caption-mono text-muted-foreground">
              <th class="py-2 pr-4">Method</th>
              <th class="py-2">Path</th>
            </tr>
          </thead>
          <tbody>
            {#each endpoints as ep}
              <tr class="border-b border-border/50">
                <td class="py-2 pr-4 font-mono text-caption">{ep.method}</td>
                <td class="py-2 font-mono">{ep.path}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Example</Card.Title>
    </Card.Header>
    <Card.Content>
      <pre class="bg-muted p-4 rounded-sm text-caption-mono overflow-x-auto"><code>curl -s {keyInfo?.base_url ?? 'http://localhost:3777/admin/api/v1'}/api-keys \\
  -H "Authorization: Bearer {keyInfo?.key ?? '<master-key>'}"</code></pre>
    </Card.Content>
  </Card.Root>
</div>
