<script lang="ts">
import { Card, CardContent } from '$lib/components/ui/card';
import { Badge } from '$lib/components/ui/badge';
import type { CLITool, CLIToolStatus } from '$lib/api';
import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';

interface Props {
	tool: CLITool;
	status?: CLIToolStatus;
	onClick: () => void;
}

let { tool, status, onClick }: Props = $props();

function getStatusBadge(s?: CLIToolStatus) {
	if (!s || !s.installed) {
		if (s?.configured) {
			return {
				label: 'Configured',
				cls: 'bg-sky-500/10 text-sky-400 border-sky-500/20 hover:bg-sky-500/10',
			};
		}
		return {
			label: 'Not configured',
			cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20 hover:bg-yellow-500/10',
		};
	}
	if (s.hasRouter) {
		return {
			label: 'Connected',
			cls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20 hover:bg-emerald-500/10',
		};
	}
	if (s.configured) {
		return {
			label: 'Configured',
			cls: 'bg-sky-500/10 text-sky-400 border-sky-500/20 hover:bg-sky-500/10',
		};
	}
	return {
		label: 'Not configured',
		cls: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20 hover:bg-yellow-500/10',
	};
}

const badge = $derived(getStatusBadge(status));
</script>

<button
	class="group flex w-full cursor-pointer text-left"
	onclick={onClick}
	aria-label={`Configure ${tool.name}`}
>
	<Card class="flex flex-1 flex-row items-center gap-4 p-4 transition-all hover:border-primary/50 hover:shadow-elevated">
		<div class="flex size-12 shrink-0 items-center justify-center rounded-lg bg-background/50">
			<img
				src={tool.image}
				alt={tool.name}
				class="size-10 rounded-lg object-contain"
				onerror={(e) => (e.currentTarget.style.display = 'none')}
			/>
		</div>
		<CardContent class="min-w-0 flex-1 p-0">
			<h3 class="truncate text-body-md-strong">{tool.name}</h3>
			<p class="line-clamp-2 text-body-sm text-muted-foreground">{tool.description}</p>
			<Badge variant="outline" class="mt-2 border px-2 py-0.5 text-[11px] font-medium {badge.cls}">
				{badge.label}
			</Badge>
		</CardContent>
		<ChevronRightIcon
			class="size-5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5"
		/>
	</Card>
</button>
