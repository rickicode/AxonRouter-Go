<script lang="ts">
import type { Component } from 'svelte';
import Play from '@lucide/svelte/icons/play';
import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
import Power from '@lucide/svelte/icons/power';
import PowerOff from '@lucide/svelte/icons/power-off';
import Trash2 from '@lucide/svelte/icons/trash-2';
import Pencil from '@lucide/svelte/icons/pencil';
import Settings2 from '@lucide/svelte/icons/settings-2';
import Copy from '@lucide/svelte/icons/copy';
import Search from '@lucide/svelte/icons/search';
import ArrowLeft from '@lucide/svelte/icons/arrow-left';
import RefreshCw from '@lucide/svelte/icons/refresh-cw';
import Plus from '@lucide/svelte/icons/plus';
import X from '@lucide/svelte/icons/x';
import Check from '@lucide/svelte/icons/check';
import ChevronDown from '@lucide/svelte/icons/chevron-down';
import ChevronRight from '@lucide/svelte/icons/chevron-right';

const icons: Record<string, Component> = {
  play: Play,
  rotateCcw: RotateCcw,
  power: Power,
  powerOff: PowerOff,
  trash2: Trash2,
  pencil: Pencil,
  settings2: Settings2,
  copy: Copy,
  search: Search,
  arrowLeft: ArrowLeft,
  refreshCw: RefreshCw,
  plus: Plus,
  x: X,
  check: Check,
  chevronDown: ChevronDown,
  chevronRight: ChevronRight,
};

let { name, class: className = '' }: { name: string; class?: string } = $props();

const IconComponent = $derived(icons[name] ?? icons.x);
</script>

<IconComponent class={className} />
