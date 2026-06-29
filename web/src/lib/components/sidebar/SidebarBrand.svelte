<script lang="ts">
  import * as Avatar from '$lib/components/ui/avatar';
</script>

<a href="/" class="flex items-center gap-3 group" aria-label="AxonRouter Dashboard">
  <Avatar.Root class="size-8">
    <Avatar.Fallback class="bg-primary text-primary-foreground text-sm font-semibold">A</Avatar.Fallback>
  </Avatar.Root>

  <div class="flex-1 min-w-0 group-data-[collapsible=icon]:hidden">
    <span class="font-medium text-base text-sidebar-foreground block truncate">
      AxonRouter
    </span>
  </div>
</a>
