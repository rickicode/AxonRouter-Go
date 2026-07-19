<script lang="ts">
  import { Button } from '$lib/components/ui/button';
  import { copyToClipboard } from '$lib/copy';
  import CopyIcon from '@lucide/svelte/icons/copy';
  import CheckIcon from '@lucide/svelte/icons/check';

  interface Props {
    code: string;
    label?: string;
    class?: string;
  }

  let { code, label = 'Code', class: className = '' }: Props = $props();

  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function onCopy() {
    const ok = await copyToClipboard(code, label);
    if (ok) {
      copied = true;
      clearTimeout(timer);
      timer = setTimeout(() => (copied = false), 2000);
    }
  }
</script>

<div class="relative {className}">
  <Button
    variant="outline"
    size="sm"
    class="absolute right-2 top-2 gap-1.5 text-body-sm rounded-sm cursor-pointer"
    onclick={onCopy}
  >
    {#if copied}
      <CheckIcon class="size-3.5" />
      Copied
    {:else}
      <CopyIcon class="size-3.5" />
      Copy
    {/if}
  </Button>
  <pre class="whitespace-pre-wrap break-all rounded-sm bg-muted p-4 pr-14 text-caption-mono"><code>{code}</code></pre>
</div>
