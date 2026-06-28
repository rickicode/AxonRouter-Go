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

  const variantClasses: Record<string, string> = {
    neutral: 'bg-hairline text-ink border border-hairline',
    subtle: 'bg-surface-dark-soft text-on-dark',
    success: 'bg-green-50 text-green-700 border border-green-200',
    warning: 'bg-yellow-50 text-yellow-700 border border-yellow-200',
    error: 'bg-red-50 text-red-700 border border-red-200',
  };

  const sizeClasses: Record<string, string> = {
    sm: 'px-sm py-xxs text-mono-caps-eyebrow rounded-sm',
    md: 'px-md py-xs text-mono-caps-label rounded-sm',
  };

  let classes = $derived(`${baseClasses} ${variantClasses[variant]} ${sizeClasses[size]}`);
</script>

<span class={classes} {...restProps}>
  {#if children}{@render children()}{/if}
</span>
