<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    variant = 'default',
    padding = 'md',
    hover = false,
    children,
    ...restProps
  }: {
    variant?: 'default' | 'dark' | 'tinted';
    padding?: 'sm' | 'md' | 'lg';
    hover?: boolean;
    children?: Snippet;
    [key: string]: any;
  } = $props();

  const baseClasses = 'rounded-sm border';

    default: 'bg-card border-border',
    dark: 'bg-background border-border text-foreground',
    tinted: 'bg-pink-500/5 border-pink-500/10',

  const paddingClasses: Record<string, string> = {
    sm: 'p-lg',
    md: 'p-2xl',
    lg: 'p-3xl',
  };

  const hoverClasses = 'hover:shadow-soft-drop transition-shadow';

  let classes = $derived(`${baseClasses} ${variantClasses[variant]} ${paddingClasses[padding]} ${hover ? hoverClasses : ''}`);
</script>

<div class={classes} {...restProps}>
  {#if children}{@render children()}{/if}
</div>
