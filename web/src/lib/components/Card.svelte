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

  const baseClasses = 'rounded-lg';

    default: 'bg-card shadow-card',
    dark: 'bg-background shadow-card text-foreground',
    tinted: 'bg-pink-500/5 shadow-card',

  const paddingClasses: Record<string, string> = {
    sm: 'p-lg',
    md: 'p-2xl',
    lg: 'p-3xl',
  };

  const hoverClasses = 'hover:shadow-card-hover transition-shadow duration-200';

  let classes = $derived(`${baseClasses} ${variantClasses[variant]} ${paddingClasses[padding]} ${hover ? hoverClasses : ''}`);
</script>

<div class={classes} {...restProps}>
  {#if children}{@render children()}{/if}
</div>
