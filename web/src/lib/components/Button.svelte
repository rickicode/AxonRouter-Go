<script lang="ts">
  export let variant: 'primary' | 'secondary' | 'outline' | 'ghost' | 'danger' = 'primary';
  export let size: 'sm' | 'md' | 'lg' = 'md';
  export let disabled = false;
  export let loading = false;
  export let type: 'button' | 'submit' | 'reset' = 'button';
  export let href: string | null = null;
  
  const baseClasses = 'inline-flex items-center justify-center font-mono uppercase transition-colors focus:outline-none focus:ring-2 focus:ring-primary/20 disabled:opacity-50 disabled:cursor-not-allowed';
  
  const variantClasses = {
    primary: 'bg-primary text-on-primary hover:bg-primary/90',
    secondary: 'bg-accent-mint text-ink hover:bg-accent-mint/90',
    outline: 'bg-canvas text-ink border border-hairline hover:bg-hairline',
    ghost: 'bg-transparent text-ink hover:bg-hairline',
    danger: 'bg-red-600 text-white hover:bg-red-700',
  };
  
  const sizeClasses = {
    sm: 'px-lg py-xs text-mono-caps-eyebrow rounded-xs',
    md: 'px-2xl py-xs text-mono-caps-button rounded-sm',
    lg: 'px-3xl py-sm text-mono-caps-button rounded-sm',
  };
  
  $: classes = `${baseClasses} ${variantClasses[variant]} ${sizeClasses[size]}`;
</script>

{#if href}
  <a
    {href}
    class={classes}
    {...$$restProps}
  >
    {#if loading}
      <svg class="animate-spin -ml-1 mr-sm h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
    {/if}
    <slot />
  </a>
{:else}
  <button
    {type}
    {disabled}
    class={classes}
    on:click
    {...$$restProps}
  >
    {#if loading}
      <svg class="animate-spin -ml-1 mr-sm h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
    {/if}
    <slot />
  </button>
{/if}
