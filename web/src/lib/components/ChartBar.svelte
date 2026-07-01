<script lang="ts">
  interface DataPoint {
    date: string;
    value: number;
    label?: string;
  }

  let {
    data = [],
    title = '',
    valuePrefix = '',
    valueSuffix = '',
    color = '#ec4899',
    height = 200,
  }: {
    data: DataPoint[];
    title?: string;
    valuePrefix?: string;
    valueSuffix?: string;
    color?: string;
    height?: number;
  } = $props();

  const pad = { top: 12, right: 8, bottom: 28, left: 8 };
  const chartW = 100; // percentage-based
  let chartH = $derived(height);

  let hovered = $state<number | null>(null);

  let maxVal = $derived(Math.max(...data.map(d => d.value), 1));
  let bars = $derived(data.map((d, i) => {
    const barW = data.length > 0 ? (chartW / data.length) * 0.7 : 0;
    const gap = data.length > 0 ? (chartW / data.length) * 0.3 : 0;
    const x = data.length > 0 ? (i / data.length) * chartW + gap / 2 : 0;
    const barH = (d.value / maxVal) * (chartH - pad.top - pad.bottom);
    const y = chartH - pad.bottom - barH;
    return { ...d, x, y, barW, barH, index: i };
  }));

  function formatVal(v: number): string {
    if (v >= 1_000_000) return `${valuePrefix}${(v / 1_000_000).toFixed(1)}M${valueSuffix}`;
    if (v >= 1_000) return `${valuePrefix}${(v / 1_000).toFixed(1)}K${valueSuffix}`;
    return `${valuePrefix}${v.toFixed(valuePrefix === '$' ? 4 : 0)}${valueSuffix}`;
  }

  function formatDate(d: string): string {
    const dt = new Date(d + 'T00:00:00');
    return dt.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  }
</script>

<div class="flex flex-col gap-2">
  {#if title}
    <h3 class="text-caption-mono text-muted-foreground uppercase">{title}</h3>
  {/if}
  <div class="relative bg-card rounded-xl border border-border p-4">
    <svg
      viewBox="0 0 {chartW} {chartH}"
      class="w-full"
      preserveAspectRatio="none"
      role="img"
      aria-label={title}
    >
      <!-- Grid lines -->
      {#each [0.25, 0.5, 0.75] as pct}
        <line
          x1="0" y1={chartH - pad.bottom - (chartH - pad.top - pad.bottom) * pct}
          x2={chartW} y2={chartH - pad.bottom - (chartH - pad.top - pad.bottom) * pct}
          stroke="currentColor" class="text-border" stroke-width="0.15" stroke-dasharray="1 1"
        />
      {/each}

      <!-- Bars -->
      {#each bars as bar, i}
        <rect
          x={bar.x} y={bar.y}
          width={bar.barW} height={Math.max(bar.barH, 0.3)}
          rx="0.4"
          fill={color}
          opacity={hovered === null || hovered === i ? 1 : 0.4}
          role="presentation"
          onmouseenter={() => hovered = i}
          onmouseleave={() => hovered = null}
        />
      {/each}

      <!-- X-axis labels (every Nth) -->
      {#each bars as bar, i}
        {#if data.length <= 15 || i % Math.ceil(data.length / 10) === 0}
          <text
            x={bar.x + bar.barW / 2}
            y={chartH - 6}
            text-anchor="middle"
            class="fill-muted-foreground"
            font-size="2.2"
          >
            {formatDate(bar.date).split(' ')[1]}
          </text>
        {/if}
      {/each}
    </svg>

    <!-- Tooltip -->
    {#if hovered !== null && bars[hovered]}
      {@const b = bars[hovered]}
      <div
        class="absolute pointer-events-none z-10 bg-popover border border-border rounded-lg px-3 py-2 shadow-elevated text-body-sm"
        style:left="{Math.min(Math.max((b.x + b.barW / 2) / chartW * 100, 15), 85)}%"
        style:top="{Math.max((b.y / chartH) * 100 - 8, 4)}%"
        style:transform="translate(-50%, -100%)"
      >
        <p class="text-caption text-muted-foreground">{formatDate(b.date)}</p>
        <p class="font-medium">{formatVal(b.value)}</p>
        {#if b.errors > 0}
          <p class="text-caption text-red-400">{b.errors} errors</p>
        {/if}
      </div>
    {/if}
  </div>
</div>
