// Svelte Stores for AxonRouter-Go Dashboard

import { writable, derived } from 'svelte/store';
import { providersApi, connectionsApi, combosApi, logsApi, dashboardApi, quotaApi, fetchApi } from './api';
import type { Provider, Connection, Combo, RequestLog, QuotaCacheEntry, QuotaCacheResponse, QuotaProviderSummary, ConnectionQuota } from './api';
import { toast } from 'svelte-sonner';
function friendlyError(err: unknown, fallback: string): string {
  const msg = err instanceof Error ? err.message : fallback;
  return msg.includes('aborted') ? 'Backend not reachable. Is the server running?' : msg;
}

// Loading state
export const isLoading = writable(false);
export const error = writable<string | null>(null);

// Dashboard stats
export const dashboardStats = writable<{
  total_connections: number;
  active_connections: number;
  total_requests_today: number;
  success_rate: number;
  providers: { id: string; name: string; connection_count: number }[];
} | null>(null);

// Providers
export const providers = writable<Provider[]>([]);
export const selectedProvider = writable<Provider | null>(null);

// Connections
export const connections = writable<Connection[]>([]);
export const selectedConnection = writable<Connection | null>(null);
export const connectionPagination = writable({
  page: 1,
  per_page: 50,
  total: 0,
  total_pages: 0,
});

// Combos
export const combos = writable<Combo[]>([]);
export const selectedCombo = writable<Combo | null>(null);

// Logs
export const logs = writable<RequestLog[]>([]);
export const logPagination = writable({
  page: 1,
  per_page: 100,
  total: 0,
  total_pages: 0,
});

// Models per provider
export const providerModels = writable<string[]>([]);
export const modelTestResults = writable<Record<string, { status: string; latency_ms: number; error?: string }>>({});

// Filters
export const connectionFilter = writable({
  status: '',
  search: '',
});

export const logFilter = writable({
  provider_id: '',
  connection_id: '',
  model_id: '',
  status_code: 0,
  start_date: '',
  end_date: '',
});

// Derived stores
export const activeProviders = derived(providers, ($providers) =>
  $providers.filter((p) => p.connection_count > 0)
);

export const providerStatusCounts = derived(providers, ($providers) => {
  const counts = {
    ready: 0,
    rate_limited: 0,
    quota_exhausted: 0,
    balance_empty: 0,
    auth_failed: 0,
    suspended: 0,
    disabled: 0,
  };
  
  $providers.forEach((provider) => {
    if (provider.status_counts) {
      Object.entries(provider.status_counts).forEach(([status, count]) => {
        counts[status as keyof typeof counts] += count as number;
      });
    }
  });
  
  return counts;
});

export const totalConnections = derived(providerStatusCounts, ($counts) =>
  Object.values($counts).reduce((sum, count) => sum + count, 0)
);

// Actions
export async function loadDashboardStats() {
  isLoading.set(true);
  error.set(null);
  
  try {
    const [statsData, providersData, logsData] = await Promise.all([
      dashboardApi.stats().catch(() => ({ total_connections: 0, status_counts: { ready: 0 }, requests_today: 0 })),
      fetchApi<{ data: unknown[] }>('/dashboard/providers').catch(() => ({ data: [] })),
      fetchApi<{ data: { status_code: number }[] }>('/dashboard/recent-logs').catch(() => ({ data: [] }))
    ]);
    
    let successRate = 100;
    const logEntries = logsData?.data ?? [];
    if (logEntries.length > 0) {
      const successful = logEntries.filter((l) => l.status_code >= 200 && l.status_code < 300).length;
      successRate = Math.round((successful / logEntries.length) * 100);
    }

    const rawProviders = (providersData?.data ?? []) as { id: string; display_name?: string; total?: number }[];
    
    dashboardStats.set({
      total_connections: statsData?.total_connections ?? 0,
      active_connections: statsData?.status_counts?.ready ?? 0,
      total_requests_today: statsData?.requests_today ?? 0,
      success_rate: successRate,
      providers: rawProviders.map((p) => ({
        id: p.id,
        name: p.display_name || p.id,
        connection_count: p.total ?? 0
      }))
    });
  } catch (err) {
    // Even on total failure, set empty stats so the page renders
    dashboardStats.set({
      total_connections: 0,
      active_connections: 0,
      total_requests_today: 0,
      success_rate: 0,
      providers: []
    });
    error.set(friendlyError(err, 'Failed to load dashboard stats'));
  } finally {
    isLoading.set(false);
  }
}

export async function loadProviders() {
  isLoading.set(true);
  error.set(null);
  
  try {
    const response = await providersApi.list();
    providers.set(response.data || []);
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load providers'));
  } finally {
    isLoading.set(false);
  }
}

export async function loadProvider(id: string) {
  isLoading.set(true);
  error.set(null);
  
  try {
    const provider = await providersApi.get(id);
    selectedProvider.set(provider);
    return provider;
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load provider'));
    return null;
  } finally {
    isLoading.set(false);
  }
}

export async function loadConnections(
  providerId: string,
  page = 1,
  perPage = 50
) {
  isLoading.set(true);
  error.set(null);
  
  try {
    const filter = { page: 1, per_page: 50, status: '', search: '' };
    connectionFilter.subscribe((f) => {
      filter.status = f.status;
      filter.search = f.search;
    })();
    
    const response = await connectionsApi.list(providerId, {
      page,
      per_page: perPage,
      status: filter.status || undefined,
      search: filter.search || undefined,
    });
    
    connections.set(response.data || []);
    if (response.pagination) {
      connectionPagination.set(response.pagination);
    }
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load connections'));
  } finally {
    isLoading.set(false);
  }
}

export async function loadConnection(id: string) {
  isLoading.set(true);
  error.set(null);
  
  try {
    const connection = await connectionsApi.get(id);
    selectedConnection.set(connection);
    return connection;
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load connection'));
    return null;
  } finally {
    isLoading.set(false);
  }
}

export async function loadCombos() {
  isLoading.set(true);
  error.set(null);
  
  try {
    const response = await combosApi.list();
    combos.set(response.data || []);
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load combos'));
  } finally {
    isLoading.set(false);
  }
}

export async function loadCombo(id: string) {
  isLoading.set(true);
  error.set(null);
  
  try {
    const response = await combosApi.get(id);
    selectedCombo.set(response.combo);
    return response.combo;
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load combo'));
    return null;
  } finally {
    isLoading.set(false);
  }
}

export async function loadProviderModels(providerId: string) {
  try {
    const response = await providersApi.models(providerId);
    providerModels.set(response.data || []);
  } catch {
    providerModels.set([]);
  }
}

export async function testProviderModel(providerId: string, model: string) {
  modelTestResults.update(r => ({ ...r, [model]: { status: 'testing', latency_ms: 0 } }));
  try {
    const result = await providersApi.testModel(providerId, model);
    modelTestResults.update(r => ({ ...r, [model]: result }));
    if (result.status === 'ok') {
      toast.success(`${model} OK (${result.latency_ms ?? 0}ms)`);
    } else {
      toast.error(`${model} failed: ${result.error ?? 'Unknown'}`);
    }
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : 'Unknown error';
    modelTestResults.update(r => ({
      ...r,
      [model]: { status: 'error', latency_ms: 0, error: errMsg }
    }));
    toast.error(`${model} failed: ${errMsg}`);
  }
}

export async function loadLogs(page = 1, perPage = 100) {
  isLoading.set(true);
  error.set(null);
  
  try {
    const filter = {
      provider_id: '',
      connection_id: '',
      model_id: '',
      status_code: 0,
      start_date: '',
      end_date: '',
    };
    
    logFilter.subscribe((f) => {
      filter.provider_id = f.provider_id;
      filter.connection_id = f.connection_id;
      filter.model_id = f.model_id;
      filter.status_code = f.status_code;
      filter.start_date = f.start_date;
      filter.end_date = f.end_date;
    })();
    
    const response = await logsApi.list({
      page,
      per_page: perPage,
      provider_id: filter.provider_id || undefined,
      connection_id: filter.connection_id || undefined,
      model_id: filter.model_id || undefined,
      status_code: filter.status_code || undefined,
      start_date: filter.start_date || undefined,
      end_date: filter.end_date || undefined,
    });
    
    logs.set(response.data || []);
    if (response.pagination) {
      logPagination.set(response.pagination);
    }
  } catch (err) {
    error.set(friendlyError(err, 'Failed to load logs'));
  } finally {
    isLoading.set(false);
  }
}

// Helper functions
export function formatTimestamp(timestamp: number): string {
  return new Date(timestamp * 1000).toLocaleString();
}

export function formatLatency(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function formatTokens(tokens: number): string {
  if (tokens < 1000) return tokens.toString();
  if (tokens < 1000000) return `${(tokens / 1000).toFixed(1)}k`;
  return `${(tokens / 1000000).toFixed(2)}M`;
}

export function formatCost(cost: number): string {
  return `$${cost.toFixed(4)}`;
}

export function getStatusColor(status: string): string {
  switch (status) {
    case 'ready': return 'text-green-600 bg-green-50';
    case 'rate_limited': return 'text-yellow-600 bg-yellow-50';
    case 'quota_exhausted': return 'text-orange-600 bg-orange-50';
    case 'balance_empty': return 'text-red-600 bg-red-50';
    case 'auth_failed': return 'text-red-600 bg-red-50';
    case 'suspended': return 'text-gray-600 bg-gray-50';
    case 'disabled': return 'text-gray-600 bg-gray-50';
    default: return 'text-gray-600 bg-gray-50';
  }
}

export function getStatusLabel(status: string): string {
  switch (status) {
    case 'ready': return 'Ready';
    case 'rate_limited': return 'Rate Limited';
    case 'quota_exhausted': return 'Quota Exhausted';
    case 'balance_empty': return 'Balance Empty';
    case 'auth_failed': return 'Auth Failed';
    case 'suspended': return 'Suspended';
    case 'disabled': return 'Disabled';
    default: return status;
  }
}

// Quota (cached from DB)
export const quotaItems = writable<QuotaCacheEntry[]>([]);
export const quotaTotal = writable(0);
export const quotaPage = writable(1);
export const quotaTotalPages = writable(1);
export const quotaLoading = writable(false);
export const quotaError = writable<string | null>(null);
export const quotaSummary = writable<QuotaProviderSummary[]>([]);

export async function loadQuota(params?: { provider?: string; search?: string; status?: string; page?: number; per_page?: number }) {
  quotaLoading.set(true);
  quotaError.set(null);
  try {
    const data = await quotaApi.list(params);
    quotaItems.set(data.items || []);
    quotaTotal.set(data.total);
    quotaPage.set(data.page);
    quotaTotalPages.set(data.total_pages);
  } catch (err) {
    quotaError.set(friendlyError(err, 'Failed to load quota'));
    toast.error('Failed to load quota data');
  } finally {
    quotaLoading.set(false);
  }
}

export async function loadQuotaSummary() {
  try {
    const data = await quotaApi.summary();
    quotaSummary.set(data.providers || []);
  } catch {
    // silent — summary is optional enhancement
  }
}

export async function refreshConnectionQuota(connId: string): Promise<ConnectionQuota | null> {
  try {
    const result = await quotaApi.refresh(connId);
    // Update the item in the cache list
    quotaItems.update(items =>
      items.map(item =>
        item.connection_id === connId
          ? { ...item, quotas: result.quotas, plan: result.plan || '', error: result.error || '', fetched_at: result.fetched_at, status: result.error ? 'error' : (result.quotas.length ? 'ok' : 'no_data') }
          : item
      )
    );
    toast.success('Quota refreshed');
    return result;
  } catch (err) {
    toast.error('Refresh failed: ' + friendlyError(err, 'Unknown error'));
    return null;
  }
}
