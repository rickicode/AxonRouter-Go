<script lang="ts">
  import { onMount } from 'svelte';
  import { router, currentPath } from '$lib/router';
  import { Button } from '$lib/components/ui/button';

  onMount(() => {
    document.title = '404 — AxonRouter';
  });

  let path = $derived($currentPath);
</script>

<div class="flex flex-1 flex-col items-center justify-center p-6 min-h-[70vh] text-center">
  <div class="relative">
    <!-- Giant 404 with mesh gradient -->
    <h1 class="text-[120px] md:text-[180px] font-bold leading-none tracking-tighter gradient-text select-none" aria-hidden="true">
      404
    </h1>
  </div>

  <div class="space-y-2 max-w-md -mt-4">
    <h2 class="text-display-md">Page not found.</h2>
    <p class="text-body-sm text-muted-foreground">
      {#if path && path !== '/'}
        <code class="text-code text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">{path}</code>
        doesn't exist or has been moved.
      {:else}
        The page you're looking for doesn't exist.
      {/if}
    </p>
  </div>

  <div class="flex items-center gap-3 mt-6">
    <Button
      variant="outline"
      size="sm"
      class="text-body-sm cursor-pointer"
      onclick={() => router.navigate('/')}
    >
      ← Back to Dashboard
    </Button>
    <Button
      variant="default"
      size="sm"
      class="text-body-sm cursor-pointer"
      onclick={() => window.history.back()}
    >
      Go Back
    </Button>
  </div>
</div>

<style>
  .gradient-text {
    background: linear-gradient(135deg, #ec4899 0%, #f472b6 30%, #818cf8 70%, #50e3c2 100%);
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
  }
</style>
