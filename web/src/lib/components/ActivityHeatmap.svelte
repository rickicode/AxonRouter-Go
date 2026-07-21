<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';

	export interface ActivityDay {
		date: string;
		requests: number;
		tokens: number;
		cost_usd: number;
	}

	type MetricKey = 'requests' | 'tokens' | 'cost_usd';

	interface MetricOption {
		value: MetricKey;
		label: string;
	}

	interface HeatmapCell {
		date: string;
		value: number;
		intensity: number;
	}

	interface Props {
		title?: string;
		days?: ActivityDay[];
		metric?: MetricKey;
		metrics?: MetricOption[];
		formatter?: (value: number, metric: MetricKey) => string;
		loading?: boolean;
		emptyMessage?: string;
		onMetricChange?: (metric: MetricKey) => void;
	}

	let {
		title = 'Activity Heatmap',
		days = [],
		metric = 'requests',
		metrics = [],
		formatter = (v: number) => v.toLocaleString(),
		loading = false,
		emptyMessage = 'No activity data for this period.',
		onMetricChange,
	}: Props = $props();

	const dayLabels = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

	const intensityClasses: Record<number, string> = {
		0: 'bg-muted',
		1: 'bg-emerald-500/15',
		2: 'bg-emerald-500/30',
		3: 'bg-emerald-500/50',
		4: 'bg-emerald-500',
	};

	function parseDateUTC(dateStr: string): Date {
		const [y, m, d] = dateStr.split('-').map(Number);
		return new Date(Date.UTC(y, m - 1, d));
	}

	function formatDateKeyUTC(date: Date): string {
		const y = date.getUTCFullYear();
		const m = String(date.getUTCMonth() + 1).padStart(2, '0');
		const d = String(date.getUTCDate()).padStart(2, '0');
		return `${y}-${m}-${d}`;
	}

	function addDaysUTC(date: Date, days: number): Date {
		return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate() + days));
	}

	function startOfWeekUTC(date: Date): Date {
		const day = date.getUTCDay();
		return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate() - day));
	}

	function endOfWeekUTC(date: Date): Date {
		const day = date.getUTCDay();
		return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate() + (6 - day)));
	}

	function intensityFor(value: number, max: number): number {
		if (value === 0 || max === 0) return 0;
		const ratio = value / max;
		if (ratio <= 0.25) return 1;
		if (ratio <= 0.5) return 2;
		if (ratio <= 0.75) return 3;
		return 4;
	}

	const processed = $derived.by(() => {
		if (loading || days.length === 0) {
			return null;
		}

		const sorted = [...days].sort((a, b) => a.date.localeCompare(b.date));
		const lookup = new Map<string, ActivityDay>();
		for (const day of sorted) {
			lookup.set(day.date, day);
		}

		const first = parseDateUTC(sorted[0].date);
		const last = parseDateUTC(sorted[sorted.length - 1].date);
		const start = startOfWeekUTC(first);
		const end = endOfWeekUTC(last);

		const cells: HeatmapCell[] = [];
		let maxValue = 0;
		let total = 0;
		let activeCount = 0;

		for (let current = new Date(start); current <= end; current = addDaysUTC(current, 1)) {
			const dateStr = formatDateKeyUTC(current);
			const day = lookup.get(dateStr);
			const value = day ? day[metric] : 0;

			if (value > maxValue) maxValue = value;
			if (value > 0) {
				total += value;
				activeCount++;
			}

			cells.push({ date: dateStr, value, intensity: 0 });
		}

		for (const cell of cells) {
			cell.intensity = intensityFor(cell.value, maxValue);
		}

		const weeks: HeatmapCell[][] = [];
		for (let i = 0; i < cells.length; i += 7) {
			weeks.push(cells.slice(i, i + 7));
		}

		let bestStreak = 0;
		let currentStreak = 0;
		for (const cell of cells) {
			if (cell.value > 0) {
				currentStreak++;
				if (currentStreak > bestStreak) bestStreak = currentStreak;
			} else {
				currentStreak = 0;
			}
		}

		return {
			weeks,
			total,
			peak: maxValue,
			activeCount,
			bestStreak,
			avgPerActiveDay: activeCount > 0 ? total / activeCount : 0,
		};
	});

	function formatLabel(dateStr: string): string {
		const date = parseDateUTC(dateStr);
		return date.toLocaleDateString('en-GB', {
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			year: 'numeric',
			timeZone: 'UTC',
		});
	}

	function cellTooltip(cell: HeatmapCell): string {
		return `${formatLabel(cell.date)}: ${formatter(cell.value, metric)}`;
	}
</script>

<Card class="shadow-card">
	<CardHeader class="pb-3 border-b border-border flex flex-wrap items-center justify-between gap-3">
		<div class="space-y-0.5">
			<CardTitle class="text-body-md-strong">{title}</CardTitle>
			{#if metrics.length > 1}
				<CardDescription class="text-caption">Select a metric to color the grid</CardDescription>
			{/if}
		</div>
		{#if metrics.length > 1 && onMetricChange}
			<div class="flex flex-wrap gap-1">
				{#each metrics as option}
					<Button
						variant={metric === option.value ? 'default' : 'outline'}
						size="sm"
						class="text-body-sm cursor-pointer rounded-sm"
						onclick={() => onMetricChange(option.value)}
					>
						{option.label}
					</Button>
				{/each}
			</div>
		{/if}
	</CardHeader>

	<CardContent class="space-y-4 py-4">
		{#if loading}
			<div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
				{#each Array(4) as _}
					<Card class="shadow-card">
						<CardContent class="p-3 space-y-2">
							<Skeleton class="h-3 w-16" />
							<Skeleton class="h-8 w-24" />
						</CardContent>
					</Card>
				{/each}
			</div>
			<div class="flex">
				<div class="flex flex-col gap-1 pr-2 pt-5">
					{#each dayLabels as label}
						<div class="h-3 flex items-center text-caption text-muted-foreground">{label}</div>
					{/each}
				</div>
				<div class="overflow-x-auto">
					<div class="flex gap-1">
						{#each Array(40) as _}
							<div class="flex flex-col gap-1">
								{#each Array(7) as _}
									<Skeleton class="size-3 rounded-sm" />
								{/each}
							</div>
						{/each}
					</div>
				</div>
			</div>
		{:else if !processed}
			<div class="flex flex-col items-center justify-center py-12 text-center">
				<p class="text-body-sm text-muted-foreground">{emptyMessage}</p>
			</div>
		{:else}
			<div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
				<Card class="shadow-card">
					<CardContent class="p-3">
						<p class="text-caption text-muted-foreground uppercase">Total</p>
						<p class="text-display-md">{formatter(processed.total, metric)}</p>
					</CardContent>
				</Card>
				<Card class="shadow-card">
					<CardContent class="p-3">
						<p class="text-caption text-muted-foreground uppercase">Peak</p>
						<p class="text-display-md">{formatter(processed.peak, metric)}</p>
					</CardContent>
				</Card>
				<Card class="shadow-card">
					<CardContent class="p-3">
						<p class="text-caption text-muted-foreground uppercase">Best streak</p>
						<p class="text-display-md">{processed.bestStreak}d</p>
					</CardContent>
				</Card>
				<Card class="shadow-card">
					<CardContent class="p-3">
						<p class="text-caption text-muted-foreground uppercase">Avg active day</p>
						<p class="text-display-md">{formatter(processed.avgPerActiveDay, metric)}</p>
					</CardContent>
				</Card>
			</div>

			<div class="flex">
				<div class="flex flex-col gap-1 pr-2 pt-5">
					{#each dayLabels as label}
						<div class="h-3 flex items-center text-caption text-muted-foreground">{label}</div>
					{/each}
				</div>
				<div class="overflow-x-auto">
					<div class="flex gap-1">
						{#each processed.weeks as week}
							<div class="flex flex-col gap-1">
								{#each week as cell}
									<button
										type="button"
										class="size-3 rounded-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none {intensityClasses[cell.intensity]}"
										title={cellTooltip(cell)}
										aria-label={cellTooltip(cell)}
									></button>
								{/each}
							</div>
						{/each}
					</div>
				</div>
			</div>
		{/if}
	</CardContent>
</Card>
