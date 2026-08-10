import { describe, it, expect, vi } from 'vitest';
import { proxyPoolsApi, type ProxyPool } from '$lib/api';

function makePool(id: string, name: string): ProxyPool {
  return {
    id,
    name,
    type: 'http',
    proxyUrl: 'http://x',
    noProxy: '',
    relayAuth: '',
    isActive: true,
    testStatus: 'active',
    lastTestedAt: null,
    lastError: null,
    responseTimeMs: null,
    createdAt: 1,
  };
}

describe('proxyPoolsApi.listAll', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetches every page and returns all pools', async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      const u = new URL(url, 'http://localhost');
      const page = u.searchParams.get('page');
      if (page === '1') {
        return {
          ok: true,
          headers: { get: () => null },
          json: () => Promise.resolve({
            data: [makePool('p1', 'a'), makePool('p2', 'b')],
            pagination: { page: 1, per_page: 100, total: 3, total_pages: 2 },
          }),
        };
      }
      if (page === '2') {
        return {
          ok: true,
          headers: { get: () => null },
          json: () => Promise.resolve({
            data: [makePool('p3', 'c')],
            pagination: { page: 2, per_page: 100, total: 3, total_pages: 2 },
          }),
        };
      }
      throw new Error('Unexpected page: ' + page);
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await proxyPoolsApi.listAll();

    expect(result).toHaveLength(3);
    expect(result.map((p) => p.id)).toEqual(['p1', 'p2', 'p3']);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const calls = fetchMock.mock.calls as [string, RequestInit][];
    expect(calls[0][0]).toContain('/proxy-pools?');
    expect(calls[0][0]).toContain('page=1');
    expect(calls[0][0]).toContain('per_page=100');
    expect(calls[1][0]).toContain('page=2');
  });

  it('handles an empty first page', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      headers: { get: () => null },
      json: () => Promise.resolve({
        data: [],
        pagination: { page: 1, per_page: 100, total: 0, total_pages: 0 },
      }),
    }));

    const result = await proxyPoolsApi.listAll();

    expect(result).toEqual([]);
  });
});

describe('proxyPoolsApi long-running test timeouts', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('healthRun, test and update override the 8s default fetch timeout', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: { get: () => null },
      json: () => Promise.resolve({ ok: true }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await proxyPoolsApi.healthRun();
    await proxyPoolsApi.test('pool-1');
    await proxyPoolsApi.update('pool-1', { name: 'x' });

    const calls = fetchMock.mock.calls as [string, RequestInit][];
    const health = calls.find(([url]) => url.includes('/health-check'));
    const test = calls.find(([url]) => url.includes('/test'));
    const update = calls.find(([url]) => url.endsWith('/pool-1'));
    expect(health).toBeDefined();
    expect(test).toBeDefined();
    expect(update).toBeDefined();
    // The backend health check runs all active pools with 4 workers and up to
    // 8s per pool, a relay test can take up to 30s server-side, and an edit
    // re-tests the proxy synchronously — all must exceed the default 8s client
    // timeout or the UI aborts before the result.
    expect((health![1] as Record<string, unknown>).timeout_ms).toBe(300000);
    expect((test![1] as Record<string, unknown>).timeout_ms).toBe(60000);
    expect((update![1] as Record<string, unknown>).timeout_ms).toBe(60000);
  });
});
