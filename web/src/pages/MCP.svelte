<script lang="ts">
  import { onMount } from 'svelte';
  import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';
  import { Textarea } from '$lib/components/ui/textarea';
  import { Badge } from '$lib/components/ui/badge';
  import { Switch } from '$lib/components/ui/switch';
  import * as Dialog from '$lib/components/ui/dialog';
  import * as Select from '$lib/components/ui/select';
  import { ScrollArea } from '$lib/components/ui/scroll-area';
  import { toast } from 'svelte-sonner';
  import { mcpApi, type MCPServer, type MCPTool } from '$lib/api';
  import { getToken } from '$lib/auth';
  import { copyToClipboard } from '$lib/copy';
  import PencilIcon from '@lucide/svelte/icons/pencil';
  import PlayIcon from '@lucide/svelte/icons/play';
  import TrashIcon from '@lucide/svelte/icons/trash';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import LinkIcon from '@lucide/svelte/icons/link';
  import WrenchIcon from '@lucide/svelte/icons/wrench';

  let servers = $state<MCPServer[]>([]);
  let tools = $state<Record<string, MCPTool[]>>({});
  let loading = $state(true);
  let showDialog = $state(false);
  let editing = $state<MCPServer | null>(null);

  let formName = $state('');
  let formCommand = $state('');
  let formArgs = $state('[]');
  let formEnv = $state('{}');
  let formEnabled = $state(true);
  let formRestartPolicy = $state<'always' | 'on-failure' | 'never'>('on-failure');
  let formMaxClients = $state('4');
  let formMaxIdleSec = $state('60');

  onMount(() => {
    document.title = 'MCP Servers — AxonRouter';
    loadServers();
  });

  async function loadServers() {
    loading = true;
    try {
      const res = await mcpApi.list();
      servers = res.data ?? [];
    } catch (err) {
      toast.error('Failed to load MCP servers: ' + (err instanceof Error ? err.message : 'Unknown'));
    } finally {
      loading = false;
    }
  }

  function resetForm(server?: MCPServer) {
    if (server) {
      editing = server;
      formName = server.name;
      formCommand = server.command;
      formArgs = JSON.stringify(server.args ?? [], null, 2);
      formEnv = JSON.stringify(server.env ?? {}, null, 2);
      formEnabled = server.enabled;
      formRestartPolicy = server.restart_policy;
      formMaxClients = String(server.max_clients ?? 4);
      formMaxIdleSec = String(server.max_idle_sec ?? 60);
    } else {
      editing = null;
      formName = '';
      formCommand = '';
      formArgs = '[]';
      formEnv = '{}';
      formEnabled = true;
      formRestartPolicy = 'on-failure';
      formMaxClients = '4';
      formMaxIdleSec = '60';
    }
  }

  function openCreate() {
    resetForm();
    showDialog = true;
  }

  function openEdit(server: MCPServer) {
    resetForm(server);
    showDialog = true;
  }

  function parseJSON<T>(raw: string, label: string): T {
    try {
      return JSON.parse(raw) as T;
    } catch {
      throw new Error(`Invalid JSON for ${label}`);
    }
  }

  async function handleSave() {
    const payload: Partial<MCPServer> = {
      name: formName.trim(),
      command: formCommand.trim(),
      args: parseJSON<string[]>(formArgs, 'arguments'),
      env: parseJSON<Record<string, string>>(formEnv, 'environment'),
      enabled: formEnabled,
      restart_policy: formRestartPolicy,
      max_clients: parseInt(formMaxClients, 10) || 4,
      max_idle_sec: parseInt(formMaxIdleSec, 10) || 60,
    };

    try {
      if (editing) {
        await mcpApi.update(editing.id, payload);
        toast.success('MCP server updated');
      } else {
        await mcpApi.create(payload);
        toast.success('MCP server created');
      }
      showDialog = false;
      await loadServers();
    } catch (err) {
      toast.error('Failed to save: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleDelete(server: MCPServer) {
    if (!confirm(`Delete MCP server "${server.name}"?`)) return;
    try {
      await mcpApi.delete(server.id);
      toast.success('MCP server deleted');
      await loadServers();
    } catch (err) {
      toast.error('Failed to delete: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleTest(server: MCPServer) {
    try {
      const res = await mcpApi.test(server.id);
      if (res.success) {
        toast.success(res.message || 'Server started successfully');
      } else {
        toast.error(res.error || 'Test failed');
      }
    } catch (err) {
      toast.error('Test failed: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  async function handleCopySSE(server: MCPServer) {
    const token = getToken() || '';
    const url = mcpApi.sseUrl(server.id, token);
    await copyToClipboard(url, 'SSE URL copied');
  }

  async function handleDiscoverTools(server: MCPServer) {
    try {
      const res = await mcpApi.tools(server.id);
      tools = { ...tools, [server.id]: res.data ?? [] };
    } catch (err) {
      toast.error('Failed to list tools: ' + (err instanceof Error ? err.message : 'Unknown'));
    }
  }

  function formatArgs(args: string[]) {
    if (!args || args.length === 0) return '—';
    const joined = args.join(' ');
    return joined.length > 40 ? joined.slice(0, 40) + '…' : joined;
  }

  function statusBadge(status?: string) {
    switch (status) {
      case 'running':
        return 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20';
      case 'error':
        return 'bg-red-500/10 text-red-500 border-red-500/20';
      default:
        return 'bg-muted text-muted-foreground border-border';
    }
  }

  $effect(() => {
    if (showDialog === false) {
      resetForm();
    }
  });
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">MCP Servers.</h1>
    <p class="text-body-sm text-muted-foreground">
      Register local MCP stdio servers and expose them to remote clients through the SSE bridge.
    </p>
  </div>

  <Card class="bg-card shadow-card rounded-xl border-border">
    <CardHeader class="flex flex-row items-center justify-between">
      <CardTitle class="text-display-md">Registered Servers</CardTitle>
      <Button size="sm" onclick={openCreate}>
        <PlusIcon class="size-4 mr-1.5" />
        Add Server
      </Button>
    </CardHeader>
    <CardContent>
      {#if loading}
        <p class="text-body-sm text-muted-foreground">Loading…</p>
      {:else if servers.length === 0}
        <p class="text-body-sm text-muted-foreground">No MCP servers registered yet.</p>
      {:else}
        <ScrollArea class="h-[500px]">
          <div class="space-y-3">
            {#each servers as server}
              <div class="rounded-xl border border-border p-4 bg-card/50 hover:bg-card transition-colors">
                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                  <div class="space-y-1">
                    <div class="flex items-center gap-2">
                      <span class="text-body-sm-strong">{server.name}</span>
                      <Badge variant="outline" class={`text-caption border ${statusBadge(server.status)}`}>
                        {server.status ?? 'stopped'}
                      </Badge>
                      {#if !server.enabled}
                        <Badge variant="outline" class="text-caption border text-muted-foreground">disabled</Badge>
                      {/if}
                    </div>
                    <p class="text-caption-mono text-muted-foreground font-mono">
                      {server.command} {formatArgs(server.args)}
                    </p>
                    <p class="text-caption text-muted-foreground">
                      restart: {server.restart_policy} · max clients: {server.max_clients} · idle: {server.max_idle_sec}s
                    </p>
                  </div>
                  <div class="flex flex-wrap items-center gap-2">
                    <Button variant="outline" size="sm" onclick={() => handleTest(server)}>
                      <PlayIcon class="size-3.5 mr-1.5" />
                      Test
                    </Button>
                    <Button variant="outline" size="sm" onclick={() => handleDiscoverTools(server)}>
                      <WrenchIcon class="size-3.5 mr-1.5" />
                      Tools
                    </Button>
                    <Button variant="outline" size="sm" onclick={() => handleCopySSE(server)}>
                      <LinkIcon class="size-3.5 mr-1.5" />
                      Copy URL
                    </Button>
                    <Button variant="outline" size="sm" onclick={() => openEdit(server)}>
                      <PencilIcon class="size-3.5 mr-1.5" />
                      Edit
                    </Button>
                    <Button variant="outline" size="sm" class="text-red-500 hover:text-red-500" onclick={() => handleDelete(server)}>
                      <TrashIcon class="size-3.5 mr-1.5" />
                      Delete
                    </Button>
                  </div>
                </div>

                {#if Array.isArray(tools[server.id]) && tools[server.id].length > 0}
                  <div class="mt-3 rounded-lg border border-border bg-background/40 p-3">
                    <p class="text-caption-strong mb-2">Discovered tools ({tools[server.id].length})</p>
                    <div class="space-y-1">
                      {#each tools[server.id] as tool}
                        <div class="text-body-sm font-mono">{tool.name}</div>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        </ScrollArea>
      {/if}
    </CardContent>
  </Card>
</div>

<Dialog.Root bind:open={showDialog}>
  <Dialog.Content class="sm:max-w-xl">
    <Dialog.Header>
      <Dialog.Title class="text-display-md">{editing ? 'Edit' : 'Add'} MCP Server</Dialog.Title>
      <Dialog.Description class="text-body-sm text-muted-foreground">
        Register a command-line MCP server that speaks JSON-RPC over stdio.
      </Dialog.Description>
    </Dialog.Header>

    <div class="grid gap-4 py-2">
      <div class="grid gap-2">
        <Label for="mcp-name">Name</Label>
        <Input id="mcp-name" bind:value={formName} placeholder="filesystem" />
      </div>

      <div class="grid gap-2">
        <Label for="mcp-command">Command</Label>
        <Input id="mcp-command" bind:value={formCommand} placeholder="npx" />
      </div>

      <div class="grid gap-2">
        <Label for="mcp-args">Arguments (JSON array)</Label>
        <Textarea id="mcp-args" bind:value={formArgs} rows={3} class="font-mono text-body-sm" />
      </div>

      <div class="grid gap-2">
        <Label for="mcp-env">Environment variables (JSON object)</Label>
        <Textarea id="mcp-env" bind:value={formEnv} rows={3} class="font-mono text-body-sm" />
      </div>

      <div class="grid grid-cols-2 gap-4">
        <div class="grid gap-2">
          <Label for="mcp-max-clients">Max concurrent clients</Label>
          <Input id="mcp-max-clients" type="number" bind:value={formMaxClients} />
        </div>
        <div class="grid gap-2">
          <Label for="mcp-max-idle">Max idle seconds</Label>
          <Input id="mcp-max-idle" type="number" bind:value={formMaxIdleSec} />
        </div>
      </div>

      <div class="grid gap-2">
        <Label for="mcp-restart">Restart policy</Label>
        <Select.Root type="single" bind:value={formRestartPolicy}>
          <Select.Trigger id="mcp-restart" class="w-full">
            {formRestartPolicy}
          </Select.Trigger>
          <Select.Content>
            <Select.Item value="always">always</Select.Item>
            <Select.Item value="on-failure">on-failure</Select.Item>
            <Select.Item value="never">never</Select.Item>
          </Select.Content>
        </Select.Root>
      </div>

      <div class="flex items-center gap-3">
        <Switch id="mcp-enabled" bind:checked={formEnabled} />
        <Label for="mcp-enabled">Enabled</Label>
      </div>
    </div>

    <Dialog.Footer>
      <Button variant="outline" onclick={() => (showDialog = false)}>Cancel</Button>
      <Button onclick={handleSave}>{editing ? 'Save Changes' : 'Create Server'}</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
