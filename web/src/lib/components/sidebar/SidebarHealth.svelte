<script lang="ts">
import { onMount } from 'svelte';
import { setMustChangePassword } from '$lib/auth';

let isOnline = $state(true);
let latencyMs = $state(1);
let version = $state('');

  const CHANGELOG_URL = 'https://github.com/rickicode/AxonRouter-Go/blob/main/CHANGELOG.md';

  onMount(() => {
    const checkHealth = async () => {
      const start = performance.now();
      try {
        const response = await fetch('/api/admin/health');
        latencyMs = Math.max(1, Math.round(performance.now() - start));
        isOnline = response.ok;
			if (response.ok) {
				const data = await response.json().catch(() => ({}));
				if (data.version) version = String(data.version);
				if (typeof data.must_change_password === 'boolean') {
					setMustChangePassword(data.must_change_password);
				}
			}
      } catch {
        isOnline = false;
        latencyMs = 0;
      }
    };

    checkHealth();
    const interval = setInterval(checkHealth, 30000);
    return () => clearInterval(interval);
  });

  function getLatencyColor(): string {
    if (!isOnline) return 'text-destructive';
    if (latencyMs <= 50) return 'text-emerald-400';
    if (latencyMs <= 200) return 'text-amber-400';
    return 'text-red-500';
  }
</script>

<div class="px-3 py-3 space-y-2">
  <!-- Status row -->
  <div class="flex items-center justify-between gap-2">
    <div class="flex items-center gap-2 min-w-0">
      <span class="relative flex size-2 shrink-0">
        {#if isOnline}
          <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-40"></span>
          <span class="relative inline-flex rounded-full size-2 bg-emerald-500"></span>
        {:else}
          <span class="relative inline-flex rounded-full size-2 bg-muted-foreground/40"></span>
        {/if}
      </span>
      <span class="text-xs font-medium text-sidebar-foreground/80 truncate">
        {isOnline ? 'Connected' : 'Offline'}
      </span>
    </div>

    {#if isOnline}
      <span class="text-[10px] font-mono {getLatencyColor()} shrink-0">
        {latencyMs}ms
      </span>
    {/if}
  </div>

  <!-- Version row -->
  <div class="flex items-center justify-between text-[10px] font-mono text-muted-foreground/50">
    <span>axonrouter</span>
    {#if version}
      <a
        href={CHANGELOG_URL}
        target="_blank"
        rel="noopener noreferrer"
        class="hover:text-primary hover:underline"
      >
        v{version}
      </a>
    {:else}
      <span>-</span>
    {/if}
  </div>
</div>
