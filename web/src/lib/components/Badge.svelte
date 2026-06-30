<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    variant = 'neutral',
    size = 'sm',
    children,
    ...restProps
  }: {
    variant?: 'neutral' | 'subtle' | 'success' | 'warning' | 'error';
    size?: 'sm' | 'md';
    children?: Snippet;
    [key: string]: any;
  } = $props();

  const baseClasses = 'inline-flex items-center font-mono uppercase';

    neutral: 'bg-zinc-800 text-zinc-200 border border-zinc-700',
    subtle: 'bg-pink-500/10 text-pink-400',
    success: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20',
    warning: 'bg-amber-500/10 text-amber-400 border border-amber-500/20',
    error: 'bg-red-500/10 text-red-400 border border-red-500/20',

  const sizeClasses: Record<string, string> = {
    sm: 'px-sm py-xxs text-mono-caps-eyebrow rounded-sm',
    md: 'px-md py-xs text-mono-caps-label rounded-sm',
  };

  let classes = $derived(`${baseClasses} ${variantClasses[variant]} ${sizeClasses[size]}`);
</script>

<span class={classes} {...restProps}>
  {#if children}{@render children()}{/if}
</span>
