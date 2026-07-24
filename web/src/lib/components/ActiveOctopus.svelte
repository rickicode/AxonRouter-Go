<script lang="ts">
	import type { ActiveRequest } from '$lib/api';
import ProviderIcon from './ProviderIcon.svelte';
import { getProviderMeta, getComboMeta } from '../provider-catalog';
import { combos } from '$lib/stores';

interface Props {
		requests: ActiveRequest[];
		maxRenderedItems?: number;
	}

	let { requests, maxRenderedItems = 15 }: Props = $props();

	interface DisplayItem {
		id: string;
		provider_type_id: string;
		count: number;
		req: ActiveRequest;
	}

let comboById = $derived(
	($combos || []).reduce<Record<string, { id: string; name: string }>>(
		(map, combo) => {
			map[combo.id] = combo;
			return map;
		},
		{}
	)
);

let comboByName = $derived(
	new Map<string, { id: string; name: string }>(
		($combos || []).map((combo) => [combo.name, combo])
	)
);

function metaFor(id: string, modelId?: string) {
	const comboByLookup = comboById[id] || (modelId ? comboByName.get(modelId) : undefined);
	if (comboByLookup) {
		return getComboMeta(comboByLookup.id, comboByLookup.name);
	}
	return (
		getProviderMeta(id) ?? {
			id,
			displayName: id,
			icon: 'network',
			textIcon: id.slice(0, 2).toUpperCase(),
			color: '#a1a1aa',
			iconFile: undefined,
			category: 'compatible',
			description: '',
			format: 'openai',
			authType: 'apikey',
			prefix: `${id}/`,
			isBuiltIn: false,
		}
	);
}

function activeProviderName(item: DisplayItem): string {
	if (item.req.target_provider_type_id) {
		return getProviderMeta(item.req.target_provider_type_id)?.displayName ?? item.req.target_provider_type_id;
	}
	return metaFor(item.provider_type_id, item.req.model_id).displayName;
}


	function buildItems(active: ActiveRequest[]): DisplayItem[] {
		if (active.length <= maxRenderedItems) {
			return active.map((req) => ({
				id: req.id,
				provider_type_id: req.provider_type_id,
				count: 1,
				req,
			}));
		}

		// Group by provider so the octopus stays readable with many requests.
		const groups = new Map<string, DisplayItem>();
		for (const req of active) {
			const existing = groups.get(req.provider_type_id);
			if (existing) {
				existing.count++;
			} else {
				groups.set(req.provider_type_id, {
					id: req.provider_type_id,
					provider_type_id: req.provider_type_id,
					count: 1,
					req,
				});
			}
		}
		return Array.from(groups.values()).slice(0, maxRenderedItems);
	}

	// Bigger canvas, small brain.
	const cx = 160;
	const cy = 160;
	const innerR = 26;
	const outerR = 122;

	interface Tentacle {
		index: number;
		path: string;
		x2: number;
		y2: number;
		item: DisplayItem;
		label: string;
	}

	let items = $derived<DisplayItem[]>(buildItems(requests));
	let isGrouped = $derived(requests.length > maxRenderedItems);

	function buildTentacles(active: DisplayItem[]): Tentacle[] {
		const n = active.length;
		if (n === 0) return [];
		return active.map((item, i) => {
			const angle = n === 1 ? -Math.PI / 2 : (i / n) * Math.PI * 2 - Math.PI / 2;
			const x1 = cx + Math.cos(angle) * innerR;
			const y1 = cy + Math.sin(angle) * innerR;
			const x2 = cx + Math.cos(angle) * outerR;
			const y2 = cy + Math.sin(angle) * outerR;
			const cpAngle = angle + 0.14;
			const cpR = (innerR + outerR) * 0.68;
			const cpX = cx + Math.cos(cpAngle) * cpR;
			const cpY = cy + Math.sin(cpAngle) * cpR;

			const meta = metaFor(item.provider_type_id, item.req.model_id);
			const displayName = activeProviderName(item);
			const label = item.count > 1 ? `${displayName} (${item.count})` : displayName;

			return {
				index: i,
				path: `M ${x1} ${y1} Q ${cpX} ${cpY} ${x2} ${y2}`,
				x2,
				y2,
				item,
				label: label.length > 18 ? `${label.slice(0, 17)}…` : label,
			};

		});
	}

	let tentacles = $derived<Tentacle[]>(buildTentacles(items));

function tooltipFor(t: Tentacle): string {
	const sourceMeta = metaFor(t.item.provider_type_id, t.item.req.model_id);
	const currentProvider = t.item.req.target_provider_type_id
		? (getProviderMeta(t.item.req.target_provider_type_id)?.displayName ?? t.item.req.target_provider_type_id)
		: sourceMeta.displayName;
	const base = sourceMeta.displayName;
	if (t.item.count === 1) {
		return `${base}\nvia ${currentProvider}\n${t.item.req.model_id}\n${t.item.req.connection_name || t.item.req.connection_id || ''}`;
	}
	return `${base}: ${t.item.count} active requests`;
}

</script>

<div class="relative flex items-center justify-center py-10">
	<svg viewBox="0 0 320 320" class="h-80 w-80">
		<!-- Faint background glow -->
		<circle {cx} {cy} r="100" fill="rgba(255,255,255,0.015)" />

		{#each tentacles as t (t.index)}
			<g class="tentacle">
				<!-- Core line -->
				<path d={t.path} fill="none" stroke="#22c55e" stroke-width="1.8" stroke-linecap="round" class="line" />

				<!-- Traveling blink dot -->
				<circle r="2.5" fill="#4ade80" class="dot">
					<animateMotion dur="1.1s" repeatCount="indefinite" path={t.path} />
				</circle>

				<!-- Endpoint -->
				<circle cx={t.x2} cy={t.y2} r="4" fill="#22c55e" class="endpoint" />

				<!-- Provider icon + full name -->
				<foreignObject x={t.x2 - 44} y={t.y2 - 20} width="88" height="56" class="overflow-visible">
					<div
						xmlns="http://www.w3.org/1999/xhtml"
						class="flex flex-col items-center justify-center"
						title={tooltipFor(t)}
					>
		<ProviderIcon meta={metaFor(t.item.provider_type_id, t.item.req.model_id)} size={26} />
						<span
							class="mt-0.5 line-clamp-2 max-w-[88px] text-center text-[9px] font-semibold leading-tight"
							style="color:#22c55e;text-shadow:0 1px 2px rgba(0,0,0,.8)"
						>
							{t.label}
						</span>
					</div>
				</foreignObject>
			</g>
		{/each}

		<!-- Center brain (small) -->
		<circle {cx} {cy} r="22" fill="#09090b" stroke="#3f3f46" stroke-width="1.5" />
		<image href="/logo.svg" x={cx - 16} y={cy - 16} width="32" height="32" />

		{#if requests.length > 0}
			<circle {cx} {cy} r="22" fill="none" stroke="#22c55e" stroke-width="1.5" class="brain-pulse" />
		{/if}
	</svg>

	<!-- Count badge -->
	<div
		class="absolute bottom-4 rounded-full px-3 py-1 text-caption-mono font-bold shadow-card {requests.length > 0
			? 'bg-foreground text-background'
				: 'bg-muted text-muted-foreground'}"
	>
		{requests.length} active
		{#if isGrouped}
			<span class="opacity-80"> · {items.length} providers</span>
		{/if}
	</div>
</div>

<style>
	.tentacle .line {
		filter: drop-shadow(0 0 4px rgba(34, 197, 94, 0.5));
		animation: linePulse 1.4s ease-in-out infinite;
	}
	.tentacle .endpoint {
		filter: drop-shadow(0 0 4px #22c55e);
		animation: epPulse 1.2s ease-in-out infinite;
	}
	.dot {
		filter: drop-shadow(0 0 3px #4ade80);
	}
	.brain-pulse {
		animation: brainPulse 1.6s ease-out infinite;
		transform-origin: 160px 160px;
	}

	@keyframes linePulse {
		0%,
		100% {
			opacity: 0.65;
		}
		50% {
			opacity: 1;
		}
	}
	@keyframes epPulse {
		0%,
		100% {
			r: 4;
			opacity: 0.8;
		}
		50% {
			r: 6;
			opacity: 1;
		}
	}
	@keyframes brainPulse {
		0% {
			r: 22;
			opacity: 0.6;
		}
		100% {
			r: 34;
			opacity: 0;
		}
	}
</style>
