<script lang="ts">
  import { onMount } from 'svelte';
  import { Badge } from '$lib/components/ui/badge';

  let isOnline = $state(true);
  let latencyMs = $state(1);
  let version = $state('0.1.0');

  onMount(() => {
    const checkHealth = async () => {
      const start = performance.now();
      try {
        const response = await fetch('/api/admin/health', { method: 'HEAD' });
        latencyMs = Math.max(1, Math.round(performance.now() - start));
        isOnline = response.ok;
      } catch {
        isOnline = false;
        latencyMs = 0;
      }
    };

    checkHealth();
    const interval = setInterval(checkHealth, 30000);
    return () => clearInterval(interval);
  });

  function getStatusVariant(): 'default' | 'secondary' | 'destructive' {
    if (!isOnline) return 'destructive';
    if (latencyMs <= 50) return 'default';
    if (latencyMs <= 200) return 'secondary';
    return 'destructive';
  }
</script>

<div class="px-3 py-3">
  <div class="flex items-center justify-between gap-2 mb-2">
    <div class="flex items-center gap-2 min-w-0">
      <div class="size-2 rounded-full shrink-0 {isOnline ? 'bg-emerald-500' : 'bg-zinc-400'}"></div>
      <span class="text-sm text-sidebar-foreground truncate group-data-[collapsible=icon]:hidden">
        {isOnline ? 'Online' : 'Offline'}
      </span>
    </div>

    {#if isOnline}
      <Badge variant={getStatusVariant()} class="text-xs group-data-[collapsible=icon]:hidden">
        {latencyMs}ms
      </Badge>
    {/if}
  </div>

  <div class="flex items-center justify-between text-xs text-muted-foreground group-data-[collapsible=icon]:hidden">
    <span>AxonRouter</span>
    <span>v{version}</span>
  </div>
</div>
