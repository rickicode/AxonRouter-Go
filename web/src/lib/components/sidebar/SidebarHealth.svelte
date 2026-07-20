<script lang="ts">
import {
  healthOnline,
  healthLatencyMs,
  healthCurrentVersion,
} from '$lib/health';

const CHANGELOG_URL = 'https://github.com/rickicode/AxonRouter-Go/blob/master/CHANGELOG.md';

function getLatencyColor(ms: number, isOnline: boolean): string {
  if (!isOnline) return 'text-destructive';
  if (ms <= 50) return 'text-emerald-400';
  if (ms <= 200) return 'text-amber-400';
  return 'text-red-500';
}
</script>

<div class="px-3 py-3 space-y-2">
  <!-- Status row -->
  <div class="flex items-center justify-between gap-2">
    <div class="flex items-center gap-2 min-w-0">
      <span class="relative flex size-2 shrink-0">
        {#if $healthOnline}
          <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-40"></span>
          <span class="relative inline-flex rounded-full size-2 bg-emerald-500"></span>
        {:else}
          <span class="relative inline-flex rounded-full size-2 bg-muted-foreground/40"></span>
        {/if}
      </span>
      <span class="text-xs font-medium text-sidebar-foreground/80 truncate">
        {$healthOnline ? 'Connected' : 'Offline'}
      </span>
    </div>

    {#if $healthOnline}
      <span class="text-[10px] font-mono {getLatencyColor($healthLatencyMs, $healthOnline)} shrink-0">
        {$healthLatencyMs}ms
      </span>
    {/if}
  </div>

  <!-- Version row -->
  <div class="flex items-center justify-between text-[10px] font-mono text-muted-foreground/50">
    <span>axonrouter</span>
    {#if $healthCurrentVersion}
      <a
        href={CHANGELOG_URL}
        target="_blank"
        rel="noopener noreferrer"
        class="hover:text-primary hover:underline"
      >
        v{$healthCurrentVersion}
      </a>
    {:else}
      <span>-</span>
    {/if}
  </div>
</div>
