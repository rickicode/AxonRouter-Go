// API Client for AxonRouter-Go Dashboard

const API_BASE = '/api/admin';

interface ApiResponse<T> {
  data: T;
  pagination?: {
    page: number;
    per_page: number;
    total: number;
    total_pages: number;
  };
}

export interface Provider {
  id: string;
  display_name: string;
  format: string;
  base_url: string;
  is_custom: boolean;
  connection_count: number;
  status_counts: {
    ready: number;
    rate_limited: number;
    quota_exhausted: number;
    balance_empty: number;
    auth_failed: number;
    suspended: number;
    disabled: number;
  };
}

export interface Connection {
  id: string;
  provider_type_id: string;
  name: string;
  auth_type: string;
  status: string;
  cooldown_until: number | null;
  last_error: string | null;
  last_error_code: number | null;
  last_success_at: number | null;
  last_failure_at: number | null;
  failure_count: number;
  capabilities: string;
  provider_specific_data: string | null;
  oauth_expires_at: number | null;
  is_active: boolean;
  created_at: number;
  updated_at: number;
}

export interface CreateConnectionPayload {
  name: string;
  auth_type?: 'api_key' | 'oauth' | 'none' | 'custom';
  api_key?: string;
}

export interface CreateConnectionResponse {
  id: string;
  name: string;
  status: string;
}

export interface BulkCreateConnectionResponse {
  created: number;
  total: number;
  failed?: number;
  errors?: string[];
}

export interface Combo {
  id: string;
  name: string;
  strategy: string;
  sticky_limit: number;
  timeout_ms: number;
  is_smart: boolean;
  smart_goal: string | null;
  is_active: boolean;
  created_at: number;
  updated_at: number;
}

export interface ComboDetailResponse {
  combo: Combo;
  steps: unknown[] | null;
}

export interface RequestLog {
  id: string;
  timestamp: number;
  connection_id: string;
  provider_type_id: string;
  model_id: string;
  combo_id: string;
  modality: string;
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  latency_ms: number;
  status_code: number;
  error_message: string;
  cost_usd: number;
  created_at: number;
}

interface Settings {
  [key: string]: string;
}

// Generic fetch wrapper with timeout
export async function fetchApi<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const url = `${API_BASE}${endpoint}`;
  
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 8000);
  
  try {
    const response = await fetch(url, {
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
      signal: controller.signal,
      ...options,
    });

    if (!response.ok) {
      const err = await response.json().catch(() => ({ message: response.statusText }));
      throw new Error(err.error || err.message || `HTTP ${response.status}`);
    }

    return response.json();
  } finally {
    clearTimeout(timeout);
  }
}

// Provider API
export const providersApi = {
  list: () => fetchApi<ApiResponse<Provider[]>>('/providers'),
  
  get: (id: string) => fetchApi<Provider>(`/providers/${id}`),
  
  create: (data: Partial<Provider>) =>
    fetchApi<Provider>('/providers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  
  update: (id: string, data: Partial<Provider>) =>
    fetchApi<Provider>(`/providers/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),
  
  delete: (id: string) =>
    fetchApi<void>(`/providers/${id}`, {
      method: 'DELETE',
    }),
  
  test: (id: string) =>
    fetchApi<{ success: boolean; message: string }>(`/providers/${id}/test`, {
      method: 'POST',
    }),

  models: (id: string) =>
    fetchApi<{ data: string[] }>(`/providers/${id}/models`),

  testModel: (id: string, model: string) =>
    fetchApi<{ status: string; status_code?: number; latency_ms: number; error?: string }>(
      `/providers/${id}/models/test`,
      { method: 'POST', body: JSON.stringify({ model }) }
    ),

  validateKey: (provider: string, apiKey: string) =>
    fetchApi<{ valid: boolean }>('/providers/validate', {
      method: 'POST',
      body: JSON.stringify({ provider, api_key: apiKey }),
    }),
};

// Connection API
export const connectionsApi = {
  list: (
    providerId: string,
    params?: {
      page?: number;
      per_page?: number;
      status?: string;
      search?: string;
    }
  ) => {
    const searchParams = new URLSearchParams();
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.per_page) searchParams.set('per_page', params.per_page.toString());
    if (params?.status) searchParams.set('status', params.status);
    if (params?.search) searchParams.set('search', params.search);
    
    const query = searchParams.toString();
    return fetchApi<ApiResponse<Connection[]>>(
      `/providers/${providerId}/connections${query ? `?${query}` : ''}`
    );
  },
  
  get: (id: string) => fetchApi<Connection>(`/connections/${id}`),
  
  create: (providerId: string, data: CreateConnectionPayload) =>
    fetchApi<CreateConnectionResponse>(`/providers/${providerId}/connections`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  bulkCreate: (providerId: string, data: { connections: { name: string; api_key: string }[] }) =>
    fetchApi<BulkCreateConnectionResponse>(
      `/providers/${providerId}/connections/bulk`,
      {
        method: 'POST',
        body: JSON.stringify(data),
      }
    ),
  
  update: (id: string, data: Partial<Connection>) =>
    fetchApi<Connection>(`/connections/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),
  
  delete: (id: string) =>
    fetchApi<void>(`/connections/${id}`, {
      method: 'DELETE',
    }),
  
  test: (id: string) =>
    fetchApi<{ success: boolean; message: string }>(`/connections/${id}/test`, {
      method: 'POST',
    }),
  
  reset: (id: string) =>
    fetchApi<Connection>(`/connections/${id}/reset`, {
      method: 'POST',
    }),

  refreshToken: (id: string) =>
    fetchApi<{ success: boolean; expires_at: number; message: string }>(`/connections/${id}/refresh`, {
      method: 'POST',
    }),
  
  bulkUpdate: (data: {
    ids: string[];
    action: 'enable' | 'disable' | 'test';
  }) =>
    fetchApi<{ success: number; failed: number }>(`/connections/bulk`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),
};


export const oauthApi = {
  start: (provider: string, providerName?: string) =>
    fetchApi<{ auth_url: string; session_id: string; port: number }>('/oauth/start', {
      method: 'POST',
      body: JSON.stringify({ provider, provider_name: providerName }),
    }),

  poll: (sessionId: string) =>
    fetchApi<{ status: string; name?: string; connection_id?: string; error?: string }>(
      `/oauth/${sessionId}/poll`
    ),

  submitCallback: (redirectUrl: string) =>
    fetchApi<{ ok: boolean }>('/oauth/callback', {
      method: 'POST',
      body: JSON.stringify({ redirect_url: redirectUrl }),
    }),
};

export interface APIKeyItem {
  id: string;
  name: string;
  key_preview: string;
  rate_limit_per_min: number;
  is_active: boolean;
  created_at: number;
}

export interface APIKeyCreateResponse {
  id: string;
  key: string;
  name: string;
  message: string;
}

export const apiKeysApi = {
  list: () => fetchApi<{ data: APIKeyItem[] }>('/api-keys'),

  create: (name?: string, rateLimit?: number) =>
    fetchApi<APIKeyCreateResponse>('/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name, rate_limit_per_min: rateLimit }),
    }),

  delete: (id: string) =>
    fetchApi<{ ok: boolean }>(`/api-keys/${id}`, {
      method: 'DELETE',
    }),

  toggle: (id: string, isActive: boolean) =>
    fetchApi<{ ok: boolean }>(`/api-keys/${id}/toggle`, {
      method: 'PATCH',
      body: JSON.stringify({ is_active: isActive }),
    }),
};

// Combo API
export const combosApi = {
  list: () => fetchApi<ApiResponse<Combo[]>>('/combos'),
  
  get: (id: string) => fetchApi<ComboDetailResponse>(`/combos/${id}`),
  
  create: (data: Partial<Combo>) =>
    fetchApi<Combo>('/combos', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  
  update: (id: string, data: Partial<Combo>) =>
    fetchApi<Combo>(`/combos/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),
  
  delete: (id: string) =>
    fetchApi<void>(`/combos/${id}`, {
      method: 'DELETE',
    }),
};

// Logs API
export const logsApi = {
  list: (params?: {
    page?: number;
    per_page?: number;
    provider_id?: string;
    connection_id?: string;
    model_id?: string;
    status_code?: number;
    start_date?: string;
    end_date?: string;
  }) => {
    const searchParams = new URLSearchParams();
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.per_page) searchParams.set('per_page', params.per_page.toString());
    if (params?.provider_id) searchParams.set('provider_id', params.provider_id);
    if (params?.connection_id) searchParams.set('connection_id', params.connection_id);
    if (params?.model_id) searchParams.set('model_id', params.model_id);
    if (params?.status_code) searchParams.set('status_code', params.status_code.toString());
    if (params?.start_date) searchParams.set('start_date', params.start_date);
    if (params?.end_date) searchParams.set('end_date', params.end_date);
    
    const query = searchParams.toString();
    return fetchApi<ApiResponse<RequestLog[]>>(`/logs${query ? `?${query}` : ''}`);
  },
};

// Settings API
export const settingsApi = {
  list: () => fetchApi<Settings>('/settings'),
  
  get: (key: string) => fetchApi<{ value: string }>(`/settings/${key}`),
  
  update: (key: string, value: string) =>
    fetchApi<{ value: string }>(`/settings/${key}`, {
      method: 'PUT',
      body: JSON.stringify({ value }),
    }),
};

// Dashboard API
export const dashboardApi = {
  stats: () =>
    fetchApi<{
      total_providers: number;
      total_connections: number;
      total_combos: number;
      status_counts: Record<string, number>;
      requests_today: number;
      tokens_today: number;
      cost_today: number;
      uptime_seconds: number;
    }>('/dashboard/stats'),
};

// Quota types
export interface QuotaItem {
  name: string;
  used: number;
  total: number;
  remaining_pct: number;
  reset_at?: string;
  unlimited: boolean;
  model_key?: string;
  family?: string;
}

export interface ConnectionQuota {
  connection_id: string;
  connection_name: string;
  provider_id: string;
  provider_name: string;
  plan?: string;
  quotas: QuotaItem[];
  message?: string;
  error?: string;
  fetched_at: number;
}

// Cached quota entry (from DB, flat structure)
export interface QuotaCacheEntry {
  id: string;
  connection_id: string;
  connection_name: string;
  provider_id: string;
  provider_name: string;
  display_name: string;
  color: string;
  icon_file: string;
  plan?: string;
  quotas: QuotaItem[];
  status: string; // ok, exhausted, unlimited, error, no_data
  error?: string;
  fetched_at: number;
}

export interface QuotaCacheResponse {
  items: QuotaCacheEntry[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface QuotaProviderSummary {
  provider_id: string;
  display_name: string;
  total: number;
  statuses: Record<string, number>;
}

// Legacy type for backward compat
export interface ProviderQuota {
  provider_id: string;
  provider_name: string;
  display_name: string;
  color: string;
  icon_file: string;
  connections: ConnectionQuota[];
}

// Quota API
export const quotaApi = {
  list: (params?: { provider?: string; search?: string; status?: string; page?: number; per_page?: number }) => {
    const qs = new URLSearchParams();
    if (params?.provider) qs.set('provider', params.provider);
    if (params?.search) qs.set('search', params.search);
    if (params?.status) qs.set('status', params.status);
    if (params?.page) qs.set('page', String(params.page));
    if (params?.per_page) qs.set('per_page', String(params.per_page));
    const q = qs.toString();
    return fetchApi<QuotaCacheResponse>(`/quota${q ? '?' + q : ''}`);
  },
  summary: () => fetchApi<{ providers: QuotaProviderSummary[] }>('/quota/summary'),
  refresh: (connId: string) => fetchApi<ConnectionQuota>(`/quota/${connId}/refresh`, { method: 'POST' }),
};

// Proxy Pool types
export interface ProxyPool {
  id: string;
  name: string;
  type: string; // http, vercel, deno, cloudflare
  proxyUrl: string;
  noProxy: string;
  relayAuth: string;
  isActive: boolean;
  testStatus: string; // untested, active, error
  lastTestedAt: string | null;
  lastError: string | null;
  responseTimeMs: number | null;
  createdAt: number;
  updatedAt: number;
}

export interface ProxyGroup {
  id: string;
  name: string;
  mode: string; // roundrobin, sticky
  stickyLimit: number;
  strictProxy: boolean;
  proxyPoolIds: string[];
  isActive: boolean;
  createdAt: number;
  updatedAt: number;
}

export interface HealthCheckResult {
  ok: boolean;
  lastHealthCheckAt: string | null;
}

// Proxy Pool API
export const proxyPoolsApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return fetchApi<{ data: ProxyPool[]; pagination: { page: number; per_page: number; total: number; total_pages: number } }>(`/proxy-pools${qs}`);
  },
  get: (id: string) => fetchApi<{ data: ProxyPool }>(`/proxy-pools/${id}`),
  create: (data: Record<string, unknown>) => fetchApi<{ data: ProxyPool }>(`/proxy-pools`, { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Record<string, unknown>) => fetchApi<{ data: ProxyPool }>(`/proxy-pools/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => fetchApi<{ ok: boolean }>(`/proxy-pools/${id}`, { method: 'DELETE' }),
  test: (id: string) => fetchApi<{ ok: boolean; status: number; error: string; elapsedMs: number; testedAt: string }>(`/proxy-pools/${id}/test`, { method: 'POST' }),
  healthGet: () => fetchApi<HealthCheckResult>('/proxy-pools/health-check'),
  healthRun: () => fetchApi<{ ok: boolean; checkedAt: string; results: unknown[]; skipped: boolean }>('/proxy-pools/health-check', { method: 'POST' }),
};

// Proxy Group API
export const proxyGroupsApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return fetchApi<{ data: ProxyGroup[]; pagination: { page: number; per_page: number; total: number; total_pages: number } }>(`/proxy-groups${qs}`);
  },
  get: (id: string) => fetchApi<{ data: ProxyGroup }>(`/proxy-groups/${id}`),
  create: (data: Record<string, unknown>) => fetchApi<{ data: ProxyGroup }>(`/proxy-groups`, { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Record<string, unknown>) => fetchApi<{ data: ProxyGroup }>(`/proxy-groups/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => fetchApi<{ ok: boolean }>(`/proxy-groups/${id}`, { method: 'DELETE' }),
};

export interface DeployResult {
  proxyPoolId: string;
  deployUrl: string;
  relayAuth: string;
  relayTest: { ok: boolean; status: number; error: string; elapsedMs: number };
}

// Proxy Deploy API
export const proxyDeployApi = {
  vercel: (data: { vercelToken: string; projectName?: string }) =>
    fetchApi<DeployResult>('/proxy-pools/vercel-deploy', { method: 'POST', body: JSON.stringify(data) }),
  deno: (data: { denoToken: string; orgDomain: string; projectName?: string }) =>
    fetchApi<DeployResult>('/proxy-pools/deno-deploy', { method: 'POST', body: JSON.stringify(data) }),
  cloudflare: (data: { cfToken: string; accountId: string; projectName?: string }) =>
    fetchApi<DeployResult>('/proxy-pools/cloudflare-deploy', { method: 'POST', body: JSON.stringify(data) }),
  generateSource: (type: string) =>
    fetchApi<{ type: string; source: string }>(`/proxy-pools/generate-source?type=${type}`),
};
