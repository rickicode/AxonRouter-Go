import { describe, it, expect, vi, beforeEach } from 'vitest';
import { logsApi } from '$lib/api';

describe('logsApi.clear', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('posts the selected retention days to the clear logs endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      headers: { get: () => null },
      json: () => Promise.resolve({ deleted: 12 }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await logsApi.clear(30);

    expect(result).toEqual({ deleted: 12 });
    const calls = fetchMock.mock.calls as [string, RequestInit][];
    expect(calls[0][0]).toBe('/api/admin/logs/clear');
    expect(calls[0][1].method).toBe('POST');
    expect(calls[0][1].body).toBe(JSON.stringify({ older_than_days: 30 }));
  });
});
