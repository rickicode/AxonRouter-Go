<script lang="ts">
  export let variant: 'default' | 'dark' | 'tinted' = 'default';
  export let padding: 'sm' | 'md' | 'lg' = 'md';
  export let hover = false;
  
  const baseClasses = 'rounded-sm border';
  
  const variantClasses = {
    default: 'bg-canvas border-hairline',
    dark: 'bg-canvas-dark border-surface-dark-soft text-on-dark',
    tinted: 'bg-accent-mint border-accent-mint/20',
  };
  
  const paddingClasses = {
    sm: 'p-lg',
    md: 'p-2xl',
    lg: 'p-3xl',
  };
  
  const hoverClasses = hover ? 'hover:shadow-soft-drop transition-shadow' : '';
  
  $: classes = `${baseClasses} ${variantClasses[variant]} ${paddingClasses[padding]} ${hoverClasses}`;
</script>

<div class={classes} {...$$restProps}>
  <slot />
</div>
