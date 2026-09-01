<script lang="ts">
  import { onMount } from 'svelte';
  import { fitnessApi, settingsApi, type FitnessListResponse, type GeoEntry } from '$lib/api';
  import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '$lib/components/ui/card';
  import { Button } from '$lib/components/ui/button';
  import { Badge } from '$lib/components/ui/badge';
  import { Input } from '$lib/components/ui/input';
  import { Switch } from '$lib/components/ui/switch';
  import { Label } from '$lib/components/ui/label';
  import { toast } from 'svelte-sonner';
  import SearchIcon from '@lucide/svelte/icons/search';
  import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
  import GlobeIcon from '@lucide/svelte/icons/globe';

  let data = $state<FitnessListResponse>({ success: true, pools: {}, geo: {}, names: {} });
  let loading = $state(true);
  let error = $state('');

  // Filters
  let providerFilter = $state('');
  let searchQuery = $state('');

  // Geo probe toggle (pool_geo_probe_enabled, default true)
  let geoProbeEnabled = $state(true);
  let geoProbeSaving = $state(false);
  let geoProbeLoaded = $state(false);

  interface Row {
    poolId: string;
    poolName: string;
    scope: string;
    provider: string;
    model: string;
    until: string;
    reason: string;
    geo?: GeoEntry;
  }

  let rows = $state<Row[]>([]);

  let providers = $derived.by(() => {
    const set = new Set<string>();
    for (const r of rows) set.add(r.provider);
    return ['All', ...Array.from(set).sort()];
  });

  let filtered = $derived.by(() => {
    let out = rows;
    if (providerFilter && providerFilter !== 'All') {
      out = out.filter(r => r.provider === providerFilter);
    }
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      out = out.filter(r =>
        r.poolName.toLowerCase().includes(q) ||
        r.scope.toLowerCase().includes(q) ||
        r.model.toLowerCase().includes(q) ||
        r.reason.toLowerCase().includes(q)
      );
    }
    return out;
  });

  function parseScope(scope: string): { provider: string; model: string } {
    const idx = scope.indexOf('::');
    if (idx < 0) return { provider: scope, model: '' };
    return { provider: scope.slice(0, idx), model: scope.slice(idx + 2) };
  }

  function formatUntil(until: string): string {
    const t = new Date(until).getTime();
    if (Number.isNaN(t)) return until;
    const diff = t - Date.now();
    if (diff <= 0) return 'expired';
    const mins = Math.floor(diff / 60000);
    if (mins < 60) return `${mins}m`;
    const hours = Math.floor(mins / 60);
    const rem = mins % 60;
    if (hours < 24) return `${hours}h ${rem}m`;
    const days = Math.floor(hours / 24);
    return `${days}d ${hours % 24}h`;
  }

  function egressLine(geo?: GeoEntry): string {
    if (!geo || !geo.ip) return '';
    const parts = [geo.ip];
    if (geo.country) parts.push(geo.country);
    if (geo.org) parts.push(geo.org);
    return parts.join(' · ');
  }

  function applyData(next: FitnessListResponse) {
    data = next;
    const out: Row[] = [];
    for (const [poolId, scopes] of Object.entries(next.pools ?? {})) {
      const geo = next.geo?.[poolId];
      for (const [scope, mark] of Object.entries(scopes)) {
        const { provider, model } = parseScope(scope);
        out.push({
          poolId,
          poolName: next.names?.[poolId] ?? poolId,
          scope,
          provider,
          model,
          until: mark.until,
          reason: mark.reason,
          geo,
        });
      }
    }
    // Most recently updated first (marks carry until timestamps).
    out.sort((a, b) => new Date(b.until).getTime() - new Date(a.until).getTime());
    rows = out;
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await fitnessApi.list();
      applyData(res);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load fitness data';
      toast.error(error);
    } finally {
      loading = false;
    }
  }

  async function loadGeoSetting() {
    try {
      const res = await settingsApi.get('pool_geo_probe_enabled');
      const v = res.value?.toLowerCase();
      geoProbeEnabled = v === 'true' || v === '1' || v === 'yes' || v === '';
    } catch {
      geoProbeEnabled = true;
    } finally {
      geoProbeLoaded = true;
    }
  }

  async function toggleGeoProbe(v: boolean) {
    geoProbeSaving = true;
    try {
      await settingsApi.update('pool_geo_probe_enabled', String(v));
      geoProbeEnabled = v;
      toast.success(v ? 'Egress geo probing enabled' : 'Egress geo probing disabled');
    } catch (e) {
      toast.error('Failed to save geo probe setting: ' + (e instanceof Error ? e.message : e));
    } finally {
      geoProbeSaving = false;
    }
  }

  async function clearRow(r: Row) {
    try {
      await fitnessApi.clear(r.poolId, r.scope);
      toast.success(`Cleared ${r.scope} for ${r.poolName}`);
      await load();
    } catch (e) {
      toast.error('Failed to clear mark: ' + (e instanceof Error ? e.message : e));
    }
  }

  async function clearAll() {
    const provider = providerFilter && providerFilter !== 'All' ? providerFilter : undefined;
    try {
      await fitnessApi.clearAll(provider);
      toast.success(provider ? `Cleared all ${provider} fitness marks` : 'Cleared all fitness marks');
      await load();
    } catch (e) {
      toast.error('Failed to clear marks: ' + (e instanceof Error ? e.message : e));
    }
  }

  onMount(() => {
    load();
    loadGeoSetting();
  });
</script>

<div class="flex flex-1 flex-col gap-6 p-6">
  <div class="space-y-1">
    <h1 class="text-display-lg">Proxy Fitness.</h1>
    <p class="text-body-sm text-muted-foreground">Pools marked unfit for a provider::model scope are skipped by smart rotation until the cooldown expires. Egress geo is captured by the periodic health probe.</p>
  </div>

  <div class="flex flex-col gap-4">
    <Card class="bg-card shadow-card rounded-xl border-border">
      <CardHeader class="flex flex-row items-center justify-between gap-4 flex-wrap">
        <div class="space-y-1">
          <CardTitle class="text-display-md flex items-center gap-2"><ShieldAlertIcon class="size-5" /> Fitness Marks</CardTitle>
          <CardDescription class="text-body-sm text-muted-foreground">{rows.length} active mark{rows.length === 1 ? '' : 's'}</CardDescription>
        </div>
        <div class="flex items-center gap-2 flex-wrap">
          <div class="relative">
            <SearchIcon class="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input bind:value={searchQuery} placeholder="Search pool, model, reason…" class="h-9 w-56 pl-8 text-body-sm" />
          </div>
          <select bind:value={providerFilter} class="h-9 rounded-md border border-border bg-background px-2 text-body-sm text-muted-foreground cursor-pointer">
            {#each providers as p}
              <option value={p}>{p}</option>
            {/each}
          </select>
          <Button variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer" onclick={clearAll} disabled={filtered.length === 0}>Clear {providerFilter && providerFilter !== 'All' ? providerFilter + ' ' : ''}Marks</Button>
        </div>
      </CardHeader>
      <CardContent>
        {#if loading}
          <div class="py-10 text-center text-body-sm text-muted-foreground">Loading fitness data…</div>
        {:else if error}
          <div class="py-10 text-center text-body-sm text-destructive">{error}</div>
        {:else if filtered.length === 0}
          <div class="py-10 text-center text-body-sm text-muted-foreground">No fitness marks{rows.length ? ' match the current filters' : ''}. Pools are marked automatically when a provider IP-limits their egress.</div>
        {:else}
          <div class="overflow-x-auto">
            <table class="w-full text-left text-body-sm">
              <thead>
                <tr class="border-b border-border text-caption-mono uppercase tracking-wider text-muted-foreground">
                  <th class="py-2 pr-4 font-medium">Provider</th>
                  <th class="py-2 pr-4 font-medium">Model / Scope</th>
                  <th class="py-2 pr-4 font-medium">Pool</th>
                  <th class="py-2 pr-4 font-medium">Egress</th>
                  <th class="py-2 pr-4 font-medium">Reason</th>
                  <th class="py-2 pr-4 font-medium">Until</th>
                  <th class="py-2 font-medium text-right">Action</th>
                </tr>
              </thead>
              <tbody>
                {#each filtered as r (r.poolId + r.scope)}
                  <tr class="border-b border-border/60 last:border-0">
                    <td class="py-2.5 pr-4">
                      <Badge variant="secondary" class="capitalize">{r.provider}</Badge>
                    </td>
                    <td class="py-2.5 pr-4 text-caption-mono">{r.model || r.scope}</td>
                    <td class="py-2.5 pr-4">
                      <span class="text-body-sm-strong">{r.poolName}</span>
                    </td>
                    <td class="py-2.5 pr-4 text-caption text-muted-foreground">
                      {#if r.geo}
                        <div class="flex items-center gap-1.5">
                          <GlobeIcon class="size-3.5 shrink-0" />
                          <span>{egressLine(r.geo)}</span>
                          {#if r.geo.isUnstable}
                            <Badge variant="outline" class="text-[10px] px-1.5 py-0">flapping</Badge>
                          {/if}
                          {#if r.geo.isDatacenter}
                            <Badge variant="outline" class="text-[10px] px-1.5 py-0">dc</Badge>
                          {/if}
                        </div>
                      {:else}
                        <span class="text-muted-foreground/50">—</span>
                      {/if}
                    </td>
                    <td class="py-2.5 pr-4">
                      <Badge variant="destructive" class="text-[11px]">{r.reason}</Badge>
                    </td>
                    <td class="py-2.5 pr-4 text-caption-mono">{formatUntil(r.until)}</td>
                    <td class="py-2.5 text-right">
                      <Button variant="ghost" size="sm" class="text-body-sm rounded-sm cursor-pointer" onclick={() => clearRow(r)}>Clear</Button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </CardContent>
    </Card>

    <Card class="bg-card shadow-card rounded-xl border-border">
      <CardHeader>
        <CardTitle class="text-display-md flex items-center gap-2"><GlobeIcon class="size-5" /> Egress Geo Probing</CardTitle>
        <CardDescription class="text-body-sm text-muted-foreground">The periodic health probe (every 30 min) captures each pool's egress IP, country, and org through the pool itself. Flapping (≥2 distinct egress IPs) and datacenter classification are derived from the probe history.</CardDescription>
      </CardHeader>
      <CardContent class="flex items-center gap-3">
        <Switch id="geo-probe" checked={geoProbeEnabled} onCheckedChange={toggleGeoProbe} disabled={!geoProbeLoaded || geoProbeSaving} />
        <Label for="geo-probe" class="text-sm font-medium cursor-pointer">{geoProbeEnabled ? 'Enabled' : 'Disabled'}</Label>
      </CardContent>
    </Card>
  </div>
</div>
