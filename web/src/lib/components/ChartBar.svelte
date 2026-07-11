<script lang="ts">
  interface DataPoint {
    date: string;
    value: number;
    label?: string;
    errors?: number;
  }

  let {
    data = [],
    title = '',
    valuePrefix = '',
    valueSuffix = '',
    color = '#ec4899',
    color2,
    height = 200,
    showErrors = false,
  }: {
    data: DataPoint[];
    title?: string;
    valuePrefix?: string;
    valueSuffix?: string;
    color?: string;
    color2?: string;
    height?: number;
    showErrors?: boolean;
  } = $props();

  let c2 = $derived(color2 ?? color);

  // SVG dimensions — real coordinate system
  const W = 760;
  const H = $derived(height);
  const pad = { top: 16, right: 12, bottom: 28, left: 44 };

  let hovered = $state<number | null>(null);

  let maxVal = $derived(Math.max(...data.map((d) => d.value), 1));
  let maxErrors = $derived(Math.max(...data.map((d) => d.errors ?? 0), 1));

  // Generate nice tick values
  let yTicks = $derived.by(() => {
    const ticks: number[] = [];
    const steps = 4;
    for (let i = 0; i <= steps; i++) {
      ticks.push((maxVal * i) / steps);
    }
    return ticks;
  });

  // Calculate point positions in SVG coords
  let points = $derived.by(() => {
    if (data.length === 0) return [];
    const innerW = W - pad.left - pad.right;
    const innerH = H - pad.top - pad.bottom;
    return data.map((d, i) => {
      const x = pad.left + (data.length === 1 ? innerW / 2 : (i / (data.length - 1)) * innerW);
      const y = pad.top + innerH - (d.value / maxVal) * innerH;
      const errY = pad.top + innerH - ((d.errors ?? 0) / maxErrors) * innerH;
      return { ...d, x, y, errY, index: i };
    });
  });

  let gradId = $derived('grad-' + (title || 'chart').replace(/\s/g, ''));

  // Build smooth path for area fill
  let areaPath = $derived.by(() => {
    if (points.length === 0) return '';
    const innerH = H - pad.top - pad.bottom;
    let path = `M ${points[0].x},${pad.top + innerH}`;
    path += ` L ${points[0].x},${points[0].y}`;
    for (let i = 1; i < points.length; i++) {
      const p0 = points[i - 1];
      const p1 = points[i];
      const cpx = (p0.x + p1.x) / 2;
      path += ` C ${cpx},${p0.y} ${cpx},${p1.y} ${p1.x},${p1.y}`;
    }
    path += ` L ${points[points.length - 1].x},${pad.top + innerH}`;
    path += ' Z';
    return path;
  });

  let linePath = $derived.by(() => {
    if (points.length === 0) return '';
    let path = `M ${points[0].x},${points[0].y}`;
    for (let i = 1; i < points.length; i++) {
      const p0 = points[i - 1];
      const p1 = points[i];
      const cpx = (p0.x + p1.x) / 2;
      path += ` C ${cpx},${p0.y} ${cpx},${p1.y} ${p1.x},${p1.y}`;
    }
    return path;
  });

  // X-axis labels: show ~6 evenly spaced
  let xLabels = $derived.by(() => {
    if (points.length === 0) return [];
    const max = 6;
    const step = Math.ceil(points.length / max);
    const labels: { x: number; text: string }[] = [];
    for (let i = 0; i < points.length; i += step) {
      labels.push({ x: points[i].x, text: formatDate(points[i].date) });
    }
    const last = points[points.length - 1];
    if (labels.length > 0 && labels[labels.length - 1].x !== last.x) {
      labels.push({ x: last.x, text: formatDate(last.date) });
    }
    return labels;
  });

  function formatVal(v: number): string {
    if (v >= 1_000_000) return `${valuePrefix}${(v / 1_000_000).toFixed(2)}M${valueSuffix}`;
    if (v >= 1_000) return `${valuePrefix}${(v / 1_000).toFixed(1)}K${valueSuffix}`;
    if (valuePrefix === '$') return `${valuePrefix}${v.toFixed(2)}${valueSuffix}`;
    return `${valuePrefix}${Math.round(v)}${valueSuffix}`;
  }

  function formatAxisVal(v: number): string {
    if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
    if (v >= 1_000) return `${(v / 1_000).toFixed(0)}K`;
    if (v === 0) return '0';
    return Math.round(v).toString();
  }

  function formatDate(d: string): string {
    const dt = new Date(d + 'T00:00:00');
    return dt.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  }

  function fmtFullDate(d: string): string {
    const dt = new Date(d + 'T00:00:00');
    return dt.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric' });
  }

  // Tooltip position: convert SVG coords to container percentage
  let tooltipPct = $derived.by(() => {
    if (hovered === null || !points[hovered]) return null;
    const p = points[hovered];
    return {
      leftPct: (p.x / W) * 100,
      topPct: (p.y / H) * 100,
      point: p,
    };
  });
</script>

<div class="flex flex-col gap-2">
  {#if title}
    <div class="flex items-center justify-between">
      <h3 class="text-caption-mono text-muted-foreground uppercase tracking-wide">{title}</h3>
      {#if data.length > 0}
        <span class="text-caption text-muted-foreground">{data.length}d</span>
      {/if}
    </div>
  {/if}

  <div class="relative bg-card rounded-xl border border-border shadow-card p-4 overflow-hidden">
    {#if data.length === 0}
      <div class="flex items-center justify-center text-muted-foreground text-body-sm" style="height: {H}px">
        No data
      </div>
    {:else}
      <svg
        {W}
        height={H}
        viewBox="0 0 {W} {H}"
        preserveAspectRatio="none"
        class="w-full block"
        role="img"
        aria-label={title}
      >
        <defs>
          <linearGradient id="area-{gradId}" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color={color} stop-opacity="0.35" />
            <stop offset="60%" stop-color={color} stop-opacity="0.08" />
            <stop offset="100%" stop-color={color} stop-opacity="0" />
          </linearGradient>
          <linearGradient id="line-{gradId}" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stop-color={c2} />
            <stop offset="100%" stop-color={color} />
          </linearGradient>
        </defs>

        <!-- Y-axis grid lines + labels -->
        {#each yTicks as tick}
          {@const y = pad.top + (H - pad.top - pad.bottom) - (tick / maxVal) * (H - pad.top - pad.bottom)}
          <line x1={pad.left} y1={y} x2={W - pad.right} y2={y} stroke="currentColor" class="text-border" stroke-width="0.5" stroke-dasharray="2 3" />
          <text x={pad.left - 8} y={y + 4} text-anchor="end" class="fill-muted-foreground" font-size="10" font-family="var(--font-mono)">
            {formatAxisVal(tick)}
          </text>
        {/each}

        <!-- Area fill -->
        <path d={areaPath} fill="url(#area-{gradId})" />

        <!-- Error bars (if showErrors) -->
        {#if showErrors}
          {#each points as p}
            {#if (p.errors ?? 0) > 0}
              <line x1={p.x} y1={pad.top + (H - pad.top - pad.bottom)} x2={p.x} y2={p.errY} stroke="#f87171" stroke-width="1.5" opacity="0.5" />
              <circle cx={p.x} cy={p.errY} r="2" fill="#f87171" opacity="0.7" />
            {/if}
          {/each}
        {/if}

        <!-- Main line -->
        <path d={linePath} fill="none" stroke="url(#line-{gradId})" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />

        <!-- Hover line + dot -->
        {#if hovered !== null && points[hovered]}
          <line x1={points[hovered].x} y1={pad.top} x2={points[hovered].x} y2={H - pad.bottom} stroke="currentColor" class="text-muted-foreground" stroke-width="0.5" opacity="0.4" stroke-dasharray="2 2" />
          <circle cx={points[hovered].x} cy={points[hovered].y} r="5" fill={color} stroke="var(--card)" stroke-width="2" />
        {/if}

        <!-- X-axis labels -->
        {#each xLabels as lbl}
          <text x={lbl.x} y={H - 8} text-anchor="middle" class="fill-muted-foreground" font-size="10" font-family="var(--font-mono)">
            {lbl.text}
          </text>
        {/each}

        <!-- Hover capture rects -->
        {#each points as p}
          <rect
            x={p.x - (W / data.length) / 2}
            y={pad.top}
            width={W / data.length}
            height={H - pad.top - pad.bottom}
            fill="transparent"
            onmouseenter={() => (hovered = p.index)}
            onmouseleave={() => (hovered = null)}
            role="presentation"
          />
        {/each}
      </svg>

      <!-- Tooltip -->
      {#if tooltipPct}
        <div
          class="absolute pointer-events-none z-10 bg-popover border border-border rounded-lg px-3 py-2 shadow-elevated text-body-sm whitespace-nowrap"
          style:left="{Math.min(Math.max(tooltipPct.leftPct, 12), 88)}%"
          style:top="{Math.max(tooltipPct.topPct - 8, 5)}%"
          style:transform="translate(-50%, -100%)"
        >
          <p class="text-caption text-muted-foreground mb-0.5">{fmtFullDate(tooltipPct.point.date)}</p>
          <p class="font-medium tabular-nums">{formatVal(tooltipPct.point.value)}</p>
          {#if (tooltipPct.point.errors ?? 0) > 0}
            <p class="text-caption text-red-400 mt-0.5">{tooltipPct.point.errors} errors</p>
          {/if}
        </div>
      {/if}
    {/if}
  </div>
</div>
