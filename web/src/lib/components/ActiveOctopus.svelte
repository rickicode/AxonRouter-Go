<script lang="ts">
  import type { ActiveRequest } from './api';
  import ProviderIcon from './ProviderIcon.svelte';
  import { getProviderMeta } from '../provider-catalog';

  interface Props {
    requests: ActiveRequest[];
  }

  let { requests }: Props = $props();

  function metaFor(id: string) {
    return getProviderMeta(id) ?? {
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
    };
  }

  // Bigger canvas, small brain.
  const cx = 160;
  const cy = 160;
  const innerR = 26;
  const outerR = 130;

  interface Tentacle {
    index: number;
    path: string;
    x2: number;
    y2: number;
    req: ActiveRequest;
  }

  function buildTentacles(active: ActiveRequest[]): Tentacle[] {
    const n = active.length;
    return active.map((req, i) => {
      const angle = n === 1 ? -Math.PI / 2 : (i / n) * Math.PI * 2 - Math.PI / 2;
      const x1 = cx + Math.cos(angle) * innerR;
      const y1 = cy + Math.sin(angle) * innerR;
      const x2 = cx + Math.cos(angle) * outerR;
      const y2 = cy + Math.sin(angle) * outerR;
      const cpAngle = angle + 0.14;
      const cpR = (innerR + outerR) * 0.68;
      const cpX = cx + Math.cos(cpAngle) * cpR;
      const cpY = cy + Math.sin(cpAngle) * cpR;
      return {
        index: i,
        path: `M ${x1} ${y1} Q ${cpX} ${cpY} ${x2} ${y2}`,
        x2,
        y2,
        req,
      };
    });
  }

  let tentacles = $derived<Tentacle[]>(buildTentacles(requests));
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

        <!-- Provider icon + name -->
        <foreignObject x={t.x2 - 40} y={t.y2 - 16} width="80" height="40" class="overflow-visible">
          <div xmlns="http://www.w3.org/1999/xhtml" class="flex flex-col items-center justify-center">
            <ProviderIcon meta={metaFor(t.req.provider_type_id)} size={26} />
            <span
              class="mt-0.5 whitespace-nowrap text-[10px] font-semibold"
              style="color:#22c55e;text-shadow:0 1px 2px rgba(0,0,0,.8)"
            >
              {t.req.provider_type_id.toUpperCase()}
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
    class="absolute bottom-4 rounded-full px-3 py-1 text-caption-mono font-bold shadow-card {requests.length > 0 ? 'bg-foreground text-background' : 'bg-muted text-muted-foreground'}"
  >
    {requests.length} active
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
    0%, 100% { opacity: 0.65; }
    50% { opacity: 1; }
  }
  @keyframes epPulse {
    0%, 100% { r: 4; opacity: 0.8; }
    50% { r: 6; opacity: 1; }
  }
  @keyframes brainPulse {
    0% { r: 22; opacity: 0.6; }
    100% { r: 34; opacity: 0; }
  }
</style>
