<script lang="ts">
  import ProviderIcon from './ProviderIcon.svelte';
  import { getProviderMeta } from '../provider-catalog';

  interface ProviderNode {
    id: string;
    connection_count?: number;
  }

  interface Props {
    providers: ProviderNode[];
    activeIds?: string[];
  streamCount?: number;
  }

  let { providers, activeIds = [], streamCount = 0 }: Props = $props();

  function metaFor(id: string) {
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

  const cx = 170;
  const cy = 170;
  const innerR = 30;
  const outerR = 132;
  const INACTIVE = '#71717a';
  const INACTIVE_SOFT = '#52525b';
  const ACTIVE = '#22c55e';
  const ACTIVE_SOFT = '#4ade80';

  interface Tentacle {
    index: number;
    path: string;
    x2: number;
    y2: number;
    id: string;
    label: string;
    active: boolean;
    count: number;
  }

  let activeSet = $derived(new Set(activeIds || []));

  let tentacles = $derived.by<Tentacle[]>(() => {
    const list = providers || [];
    const n = list.length;
    if (n === 0) return [];
    return list.map((p, i) => {
      const angle = n === 1 ? -Math.PI / 2 : (i / n) * Math.PI * 2 - Math.PI / 2;
      const x1 = cx + Math.cos(angle) * innerR;
      const y1 = cy + Math.sin(angle) * innerR;
      const x2 = cx + Math.cos(angle) * outerR;
      const y2 = cy + Math.sin(angle) * outerR;
      const cpAngle = angle + 0.14;
      const cpR = (innerR + outerR) * 0.68;
      const cpX = cx + Math.cos(cpAngle) * cpR;
      const cpY = cy + Math.sin(cpAngle) * cpR;
      const meta = metaFor(p.id);
      const name = meta.displayName;
      const label = name.length > 16 ? `${name.slice(0, 15)}…` : name;
      return {
        index: i,
        path: `M ${x1} ${y1} Q ${cpX} ${cpY} ${x2} ${y2}`,
        x2,
        y2,
        id: p.id,
        label,
        active: activeSet.has(p.id),
        count: p.connection_count || 0,
      };
    });
  });

  let activeCount = $derived(tentacles.filter((t) => t.active).length);

  function tooltipFor(t: Tentacle): string {
    const meta = metaFor(t.id);
    return `${meta.displayName}\n${t.count} account(s)${t.active ? '\n● streaming now' : ''}`;
  }
</script>

<div class="relative flex items-center justify-center py-12">
  <!-- Brand-tinted radial backdrop -->
  <div
    class="pointer-events-none absolute inset-0 rounded-xl bg-[radial-gradient(circle_at_center,rgba(236,72,153,0.07),transparent_70%)]"
  ></div>

  <svg viewBox="0 0 340 340" class="relative h-auto w-full max-w-[460px]">
    <defs>
      <radialGradient id="brainGlow" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#27272a" />
        <stop offset="100%" stop-color="#09090b" />
      </radialGradient>
      <radialGradient id="halo" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="rgba(236,72,153,0.10)" />
        <stop offset="100%" stop-color="rgba(236,72,153,0)" />
      </radialGradient>
    </defs>

    <!-- Depth halos -->
    <circle {cx} {cy} r="150" fill="url(#halo)" />
    <circle {cx} {cy} r="120" fill="rgba(255,255,255,0.015)" />

    {#each tentacles as t (t.index)}
      <g class="tentacle" class:active={t.active}>
        <!-- Core line: gray by default, green when a stream is active to this provider -->
        <path
          d={t.path}
          fill="none"
          stroke={t.active ? ACTIVE : INACTIVE}
          stroke-width="1.8"
          stroke-linecap="round"
          class="line"
        />
        <!-- Traveling blink dot only while streaming -->
        {#if t.active}
          <circle r="2.5" fill={ACTIVE_SOFT} class="dot">
            <animateMotion dur="1.1s" repeatCount="indefinite" path={t.path} />
          </circle>
        {/if}
        <!-- Endpoint node chip -->
        <circle
          cx={t.x2}
          cy={t.y2}
          r="17"
          fill="#18181b"
          stroke={t.active ? ACTIVE : INACTIVE_SOFT}
          stroke-width="1.5"
          class="endpoint"
        />
        <!-- Provider icon -->
        <foreignObject x={t.x2 - 17} y={t.y2 - 17} width="34" height="34" class="overflow-visible">
          <div
            xmlns="http://www.w3.org/1999/xhtml"
            class="flex h-[34px] w-[34px] items-center justify-center"
            title={tooltipFor(t)}
          >
            <ProviderIcon meta={metaFor(t.id)} size={26} />
          </div>
        </foreignObject>
        <!-- Label + account count -->
        <text
          x={t.x2}
          y={t.y2 + 33}
          text-anchor="middle"
          class="node-label"
          fill={t.active ? ACTIVE : '#a1a1aa'}
        >
          {t.label}
        </text>
        {#if t.count > 0}
          <text x={t.x2} y={t.y2 + 47} text-anchor="middle" class="node-count" fill="#71717a">
            {t.count} acct
          </text>
        {/if}
      </g>
    {/each}

    <!-- Center brain (axonrouter) -->
    <circle {cx} {cy} r="26" fill="url(#brainGlow)" stroke="#3f3f46" stroke-width="1.5" />
    <image href="/logo.svg" x={cx - 18} y={cy - 18} width="36" height="36" />
    <!-- Gentle always-on breathing ring -->
    <circle {cx} {cy} r="26" fill="none" stroke={activeCount > 0 ? ACTIVE : '#52525b'} stroke-width="1.5" class="brain-pulse" />
    {#if activeCount > 0}
      <circle {cx} {cy} r="26" fill="none" stroke={ACTIVE} stroke-width="1.5" class="brain-pulse-strong" />
    {/if}
  </svg>

  <!-- Count badge -->
  <div
    class="absolute bottom-4 rounded-full px-3 py-1 text-caption-mono font-bold shadow-card {activeCount > 0
      ? 'bg-foreground text-background'
      : 'bg-muted text-muted-foreground'}"
  >
    {providers.length} providers
    {#if activeCount > 0}<span class="opacity-80"> · {activeCount} streaming</span>{/if}
    {#if streamCount > 0}<span class="opacity-80"> · {streamCount} active streams</span>{/if}
  </div>
</div>

<style>
  .tentacle .line {
    filter: drop-shadow(0 0 4px rgba(113, 113, 122, 0.35));
  }
  .tentacle.active .line {
    filter: drop-shadow(0 0 5px rgba(34, 197, 94, 0.55));
    animation: linePulse 1.4s ease-in-out infinite;
  }
  .tentacle.active .endpoint {
    filter: drop-shadow(0 0 5px #22c55e);
    animation: epPulse 1.2s ease-in-out infinite;
  }
  .dot {
    filter: drop-shadow(0 0 3px #4ade80);
  }
  .node-label {
    font-size: 10px;
    font-weight: 600;
    text-shadow: 0 1px 2px rgba(0, 0, 0, 0.85);
  }
  .node-count {
    font-size: 8px;
    font-weight: 500;
  }
  .brain-pulse {
    animation: brainBreath 3.2s ease-in-out infinite;
    transform-origin: 170px 170px;
  }
  .brain-pulse-strong {
    animation: brainPulse 1.6s ease-out infinite;
    transform-origin: 170px 170px;
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
      r: 16;
      opacity: 0.85;
    }
    50% {
      r: 18;
      opacity: 1;
    }
  }
  @keyframes brainBreath {
    0%,
    100% {
      opacity: 0.35;
      r: 26;
    }
    50% {
      opacity: 0.6;
      r: 30;
    }
  }
  @keyframes brainPulse {
    0% {
      r: 26;
      opacity: 0.6;
    }
    100% {
      r: 40;
      opacity: 0;
    }
  }
</style>
