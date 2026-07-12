<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';

  interface Props {
    status: 'active' | 'inactive' | 'online' | 'healthy' | 'idle' | 'disabled' | 'error' | 'testing' | 'smart' | 'default' | string;
    label?: string;
    class?: string;
    title?: string;
    onclick?: (e: MouseEvent) => void;
  }

  let { status, label, class: className = '', title, onclick }: Props = $props();

  const styles: Record<string, string> = {
    active: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
    healthy: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
    online: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
    inactive: 'bg-zinc-500/15 text-zinc-500 border-zinc-500/20',
    idle: 'bg-zinc-500/15 text-zinc-500 border-zinc-500/20',
    disabled: 'bg-zinc-500/15 text-zinc-500 border-zinc-500/20',
    error: 'bg-red-500/15 text-red-400 border-red-500/30',
    testing: 'bg-yellow-500/15 text-yellow-400 border-yellow-500/30',
    smart: 'bg-violet-500/15 text-violet-400 border-violet-500/30',
    default: 'bg-sky-500/15 text-sky-400 border-sky-500/30',
  };

  const dot: Record<string, string> = {
    active: 'bg-emerald-400',
    healthy: 'bg-emerald-400',
    online: 'bg-emerald-400',
    inactive: 'bg-zinc-600',
    idle: 'bg-zinc-600',
    disabled: 'bg-zinc-600',
    error: 'bg-red-400',
    testing: 'bg-yellow-400',
    smart: 'bg-violet-400',
    default: 'bg-sky-400',
  };
</script>

<Badge
  variant="outline"
  class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase border {styles[status] || styles.default} {className} {typeof onclick === 'function' ? 'cursor-pointer' : ''}"
  {onclick}
  {title}
>
  <span class="size-1.5 rounded-full {dot[status] || dot.default}"></span>
  {label ?? status}
</Badge>
