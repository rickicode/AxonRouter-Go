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

  function initials(value: string | undefined): string {
    if (!value) return 'AI';
    const words = value.replace(/[^a-zA-Z0-9 ]/g, ' ').trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) return 'AI';
    if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
    return `${words[0][0]}${words[1][0]}`.toUpperCase();
  }

  function hexToRgba(color: string | undefined, alpha: number): string {
    const value = color?.trim();
    if (!value || !/^#[0-9a-fA-F]{6}$/.test(value)) return `rgb(245 245 245 / ${alpha})`;
    const r = parseInt(value.slice(1, 3), 16);
    const g = parseInt(value.slice(3, 5), 16);
    const b = parseInt(value.slice(5, 7), 16);
    return `rgb(${r} ${g} ${b} / ${alpha})`;
  }
</script>

{#if meta?.iconFile && !imgError}
  <img
    src={meta.iconFile}
    alt={meta.displayName}
    width={size}
    height={size}
    class="rounded-md object-contain {className}"
    style="width: {size}px; height: {size}px;"
    onerror={() => (imgError = true)}
  />
{:else}
  <div
    class="flex items-center justify-center rounded-md font-mono font-semibold tracking-[-0.04em] {className}"
    style="width: {size}px; height: {size}px; font-size: {Math.max(10, Math.round(size * 0.34))}px; color: {meta?.color ?? '#171717'}; background: {hexToRgba(meta?.color, 0.12)};"
    aria-label={meta?.displayName ?? 'Provider'}
  >
    {meta?.textIcon ?? initials(meta?.displayName)}
  </div>
{/if}
