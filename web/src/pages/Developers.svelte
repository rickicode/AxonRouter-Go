<script lang="ts">
  import { onMount } from 'svelte';
  import { toast } from 'svelte-sonner';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import * as Card from '$lib/components/ui/card';
  import CodeBlock from '$lib/components/CodeBlock.svelte';
  import { developersApi } from '$lib/api';
  import { copyToClipboard } from '$lib/copy';
  import CopyIcon from '@lucide/svelte/icons/copy';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
  import CodeIcon from '@lucide/svelte/icons/code';
  import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';

  type MasterKeyInfo = {
    key: string;
    prefix: string;
    base_url: string;
    created_at: number;
  };

  let keyInfo: MasterKeyInfo | null = $state(null);
  let loading = $state(false);
  let error = $state<string | null>(null);

  async function loadKey() {
    loading = true;
    error = null;
    try {
      const res = await developersApi.getMasterKey();
      keyInfo = res.data;
    } catch (err: any) {
      error = err.message || 'Failed to load master key';
      toast.error(error ?? 'Failed to load master key');
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

  async function copy(text: string, label: string) {
    await copyToClipboard(text, label);
  }

  // Build a shell-ready curl command for a given method/path/body.
  function curlCmd(method: string, path: string, body?: string): string {
    const base = keyInfo?.base_url ?? 'http://localhost:3777/admin/api/v1';
    const key = keyInfo?.key ?? '<master-key>';
    const lines = [
      `curl -s -X ${method} ${base}${path} \\`,
      `  -H "Authorization: Bearer ${key}" \\`,
    ];
    if (method !== 'GET') lines.push(`  -H "Content-Type: application/json" \\`);
    if (body) lines.push(`  -d '${body}'`);
    return lines.join('\n');
  }

  const createKeyResponse = `{
  "id": "ax-xxxx...",
  "key": "ax-xxxx...",
  "name": "my-app",
  "max_tokens": 10000000,
  "expires_at": 1784782236,
  "message": "Save this key — it won't be shown again"
}`;

  const listKeysResponse = `{
  "data": [
    {
      "id": "ax-xxxx...",
      "name": "my-app",
      "key": "ax-xxxx...",
      "rate_limit_per_min": 600,
      "max_tokens": 10000000,
      "is_active": true,
      "created_at": 1753497600,
      "expires_at": 1784782236
    }
  ]
}`;

  const toggleKeyResponse = `{
  "data": { "ok": true }
}`;
  const deleteKeyResponse = `{
  "data": { "ok": true }
}`;

  const proxyUsageRequest = `curl -s -X POST http://localhost:3777/v1/chat/completions \\
  -H "Authorization: Bearer <proxy-key>" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"openai/gpt-4o","messages":[{"role":"user","content":"Hello"}]}'`;

  const proxyUsageResponse = `{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "model": "openai/gpt-4o",
  "choices": [{"message": {"role": "assistant", "content": "Hi!"}}]
}`;

  const createProviderBody = '{"name":"myprovider","display_name":"My Provider","format":"openai","base_url":"https://api.example.com/v1"}';

  const createProviderResponse = `{
  "id": "myprovider",
  "display_name": "My Provider",
  "format": "openai",
  "base_url": "https://api.example.com/v1",
  "is_custom": true,
  "category": "compatible",
  "service_kinds": ["llm"]
}`;

  const addConnectionBody = '{"name":"primary","api_key":"sk-...","auth_type":"api_key","priority":0}';

  const addConnectionResponse = `{
  "id": "conn-uuid",
  "name": "primary",
  "status": "ready"
}`;

  const bulkAddConnectionsBody = '{"connections":[{"name":"akun-1","api_key":"sk-...","priority":0},{"name":"akun-2","api_key":"sk-...","priority":0}]}';

  const bulkAddConnectionsResponse = `{
  "created": 2,
  "total": 2,
  "failed": 0,
  "errors": []
}`;

  const validateKeyBody = '{"provider":"openai","api_key":"sk-..."}';

  const validateKeyResponse = `{
  "valid": true
}`;

  const importOAuthBody = '{"provider":"grok-cli","access_token":"...","refresh_token":"...","expires_at":1754726400,"email":"ops@example.com"}';

  const importOAuthResponse = `{
  "id": "conn-uuid",
  "name": "ops@example.com",
  "status": "ready"
}`;

  const startOAuthBody = '{"provider":"grok-cli"}';

  const startOAuthResponse = `{
  "auth_url": "http://localhost:PORT/auth?response_type=code&...",
  "session_id": "sess-xxxx...",
  "port": 31123,
  "user_code": "ABCD-EFGH"
}`;

  const pollOAuthResponse = `{
  "status": "connected",
  "name": "ops@example.com",
  "connection_id": "conn-uuid",
  "error": ""
}`;

  const submitCallbackBody = '{"redirect_url":"http://localhost:PORT/auth/callback?code=...&state=..."}';

  const endpoints = [
    { method: 'GET', path: '/admin/api/v1/providers' },
    { method: 'POST', path: '/admin/api/v1/providers' },
    { method: 'POST', path: '/admin/api/v1/providers/:id/connections' },
    { method: 'POST', path: '/admin/api/v1/providers/:id/connections/bulk' },
    { method: 'POST', path: '/admin/api/v1/providers/validate' },
    { method: 'POST', path: '/admin/api/v1/oauth/start' },
    { method: 'GET', path: '/admin/api/v1/oauth/:sessionId/poll' },
    { method: 'POST', path: '/admin/api/v1/oauth/callback' },
    { method: 'POST', path: '/admin/api/v1/oauth/import-token' },
    { method: 'GET', path: '/admin/api/v1/api-keys' },
    { method: 'POST', path: '/admin/api/v1/api-keys' },
    { method: 'GET', path: '/admin/api/v1/logs' },
    { method: 'GET', path: '/admin/api/v1/model-pricing' },
    { method: 'GET', path: '/admin/api/v1/settings' },
  ];

  onMount(() => {
    document.title = 'Developers — AxonRouter';
    loadKey();
  });
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
      {#if loading}
        <div class="space-y-6">
          <div class="space-y-2">
            <div class="h-4 w-12 animate-pulse rounded bg-muted"></div>
            <div class="flex gap-2">
              <div class="h-10 flex-1 animate-pulse rounded bg-muted"></div>
              <div class="size-10 animate-pulse rounded bg-muted"></div>
            </div>
            <div class="h-3 w-48 animate-pulse rounded bg-muted"></div>
          </div>
          <div class="space-y-2">
            <div class="h-4 w-16 animate-pulse rounded bg-muted"></div>
            <div class="flex gap-2">
              <div class="h-10 flex-1 animate-pulse rounded bg-muted"></div>
              <div class="size-10 animate-pulse rounded bg-muted"></div>
            </div>
          </div>
          <div class="h-10 w-32 animate-pulse rounded bg-muted"></div>
        </div>
      {:else if error}
        <div class="flex flex-col items-center justify-center gap-3 py-6 text-center">
          <AlertTriangleIcon class="size-8 text-destructive" />
          <p class="text-body-sm text-muted-foreground">{error}</p>
          <Button onclick={loadKey} variant="outline" class="text-body-sm rounded-sm">Try again</Button>
        </div>
      {:else if keyInfo}
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
      {/if}
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Endpoints</Card.Title>
      <Card.Description>Common <code>/admin/api/v1</code> paths for gateway automation.</Card.Description>
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
      <Card.Title class="text-display-md">Quick example</Card.Title>
      <Card.Description>List the admin API keys currently registered.</Card.Description>
    </Card.Header>
    <Card.Content>
      <CodeBlock code={curlCmd('GET', '/api-keys')} label="List API keys" />
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Providers &amp; connections</Card.Title>
      <Card.Description>
        Bootstrap custom providers and load credentials automatically with the master key.
      </Card.Description>
    </Card.Header>
    <Card.Content class="space-y-6">
      <div class="space-y-2">
        <p class="text-body-sm font-medium">1. Create a custom provider</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/providers</code></p>
        <CodeBlock code={curlCmd('POST', '/providers', createProviderBody)} label="Create provider" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={createProviderBody} label="Request body" />
        <p class="text-body-sm text-muted-foreground">Response <code>201 Created</code>:</p>
        <CodeBlock code={createProviderResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">2. Add a credential (API key)</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/providers/&#123;id&#125;/connections</code></p>
        <CodeBlock code={curlCmd('POST', '/providers/PROVIDER_ID/connections', addConnectionBody)} label="Add connection" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={addConnectionBody} label="Request body" />
        <p class="text-body-sm text-muted-foreground">Response <code>201 Created</code>:</p>
        <CodeBlock code={addConnectionResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">3. Bulk add credentials</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/providers/&#123;id&#125;/connections/bulk</code> &mdash; up to 5.000 per call</p>
        <CodeBlock code={curlCmd('POST', '/providers/PROVIDER_ID/connections/bulk', bulkAddConnectionsBody)} label="Bulk add connections" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={bulkAddConnectionsBody} label="Request body" />
        <p class="text-body-sm text-muted-foreground">Response <code>201 Created</code>:</p>
        <CodeBlock code={bulkAddConnectionsResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">4. Validate a key before storing</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/providers/validate</code></p>
        <CodeBlock code={curlCmd('POST', '/providers/validate', validateKeyBody)} label="Validate key" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={validateKeyBody} label="Request body" />
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code>:</p>
        <CodeBlock code={validateKeyResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">5. Import an OAuth token</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/oauth/import-token</code></p>
        <CodeBlock code={curlCmd('POST', '/oauth/import-token', importOAuthBody)} label="Import OAuth token" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={importOAuthBody} label="Request body" />
        <p class="text-body-sm text-muted-foreground">Response <code>201 Created</code>:</p>
        <CodeBlock code={importOAuthResponse} label="Response" />
      </div>
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Add an OAuth account</Card.Title>
      <Card.Description>
        Start an OAuth flow, let the user authorize in the browser, then poll until the
        connection is created. No orphaned connections are left on failure.
      </Card.Description>
    </Card.Header>
    <Card.Content class="space-y-6">
      <div class="space-y-2">
        <p class="text-body-sm font-medium">1. Start the OAuth flow</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/oauth/start</code></p>
        <CodeBlock code={curlCmd('POST', '/oauth/start', startOAuthBody)} label="Start OAuth" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={startOAuthBody} label="Request body" />
        <p class="text-body-sm text-muted-foreground">
          Only <code>provider</code> is required. The eventual connection name is taken from the
          authorized OAuth account email; if no email is returned it falls back to
          <code>OAuth &lt;provider&gt;</code>. You can optionally send <code>provider_name</code> to
          override the fallback label.
        </p>
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code> &mdash; open <code>auth_url</code> and finish login:</p>
        <CodeBlock code={startOAuthResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">2. Poll the session status</p>
        <p class="text-body-sm text-muted-foreground">GET <code>/admin/api/v1/oauth/:sessionId/poll</code></p>
        <CodeBlock code={curlCmd('GET', '/oauth/SESSION_ID/poll')} label="Poll OAuth status" />
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code> &mdash; repeat until <code>status</code> is <code>connected</code> or <code>failed</code>:</p>
        <CodeBlock code={pollOAuthResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">3. Submit the callback (remote dashboards)</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/oauth/callback</code></p>
        <p class="text-body-sm text-muted-foreground">
          If the gateway runs on another machine, paste the localhost callback URL the
          provider redirected to:
        </p>
        <CodeBlock code={curlCmd('POST', '/oauth/callback', submitCallbackBody)} label="Submit callback" />
        <p class="text-body-sm text-muted-foreground">Request body:</p>
        <CodeBlock code={submitCallbackBody} label="Request body" />
      </div>
    </Card.Content>
  </Card.Root>

  <Card.Root class="shadow-card">
    <Card.Header>
      <Card.Title class="text-display-md">Proxy API keys via master key</Card.Title>
      <Card.Description>
        The master key can also create the proxy API keys used for <code>/v1/*</code> requests.
      </Card.Description>
    </Card.Header>
    <Card.Content class="space-y-6">
      <div class="space-y-2">
        <p class="text-body-sm font-medium">1. Create an API key</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/admin/api/v1/api-keys</code></p>
        <CodeBlock code={curlCmd('POST', '/api-keys', '{"name":"my-app","rate_limit_per_min":600,"max_tokens":10000000,"expires_at":1784782236}')} label="Create API key" />
        <p class="text-body-sm text-muted-foreground">Response <code>201 Created</code>:</p>
        <CodeBlock code={createKeyResponse} label="Response" />
        <p class="text-caption text-muted-foreground">Use the returned <code>key</code> in <code>Authorization: Bearer &lt;key&gt;</code> for <code>/v1/*</code>.</p>
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">2. List API keys</p>
        <p class="text-body-sm text-muted-foreground">GET <code>/admin/api/v1/api-keys</code></p>
        <CodeBlock code={curlCmd('GET', '/api-keys')} label="List API keys" />
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code>:</p>
        <CodeBlock code={listKeysResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">3. Disable / enable an API key</p>
        <p class="text-body-sm text-muted-foreground">PATCH <code>/admin/api/v1/api-keys/&#123;id&#125;/toggle</code></p>
        <CodeBlock code={curlCmd('PATCH', '/api-keys/ax-xxxx.../toggle', '{"is_active":false,"max_tokens":10000000}')} label="Toggle API key" />
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code>:</p>
        <CodeBlock code={toggleKeyResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">4. Delete an API key</p>
        <p class="text-body-sm text-muted-foreground">DELETE <code>/admin/api/v1/api-keys/&#123;id&#125;</code></p>
        <CodeBlock code={curlCmd('DELETE', '/api-keys/ax-xxxx...')} label="Delete API key" />
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code>:</p>
        <CodeBlock code={deleteKeyResponse} label="Response" />
      </div>

      <div class="space-y-2">
        <p class="text-body-sm font-medium">5. Use the proxy API key</p>
        <p class="text-body-sm text-muted-foreground">POST <code>/v1/chat/completions</code></p>
        <CodeBlock code={proxyUsageRequest} label="Proxy request" />
        <p class="text-body-sm text-muted-foreground">Response <code>200 OK</code> (OpenAI-compatible):</p>
        <CodeBlock code={proxyUsageResponse} label="Proxy response" />
        <p class="text-caption text-muted-foreground">Model ID harus menyertakan prefix provider, misalnya <code>openai/gpt-4o</code>, <code>claude/claude-sonnet-4</code>, atau <code>cx/gpt-5.4</code>.</p>
      </div>
    </Card.Content>
  </Card.Root>
</div>
