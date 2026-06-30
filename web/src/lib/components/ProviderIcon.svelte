<script lang="ts">
  import type { ProviderMeta } from '$lib/provider-catalog';

  let {
    meta,
    size = 36,
    class: className = '',
  }: {
    meta: ProviderMeta | undefined;
    size?: number;
    class?: string;
  } = $props();

  let imgError = $state(false);
</script>

{#if meta?.iconFile && !imgError}
  <img
    src={meta.iconFile}
    alt={meta.displayName}
    width={size}
    height={size}
    class="rounded-md object-contain {className}"
    style="width: {size}px; height: {size}px;"
    onerror={() => imgError = true}
  />
{:else}
  <div
    class="flex items-center justify-center rounded-md {className}"
    style="width: {size}px; height: {size}px; font-size: {Math.round(size * 0.55)}px;"
  >
    {meta?.icon ?? '🔧'}
  </div>
{/if}
