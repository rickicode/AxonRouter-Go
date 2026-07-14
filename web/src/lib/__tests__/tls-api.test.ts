import { describe, it, expect, vi, beforeEach } from 'vitest';
import { tlsApi } from '$lib/api';

describe('tlsApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  function mockFetch(response: unknown) {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: () => Promise.resolve(response),
      }),
    );
  }

  it('get fetches /api/admin/tls-config and returns the wrapper', async () => {
    const payload = {
      data: {
        enabled: true,
        domain: 'example.com',
        email: 'admin@example.com',
        acceptTOS: true,
        staging: false,
        certCache: 'certs',
        valid: true,
        certDir: '/data/certs',
      },
    };
    mockFetch(payload);

    const result = await tlsApi.get();
    expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0]).toBe('/api/admin/tls-config');
    expect(result).toEqual(payload);
  });

  it('save PUTs the payload to /api/admin/tls-config', async () => {
    const payload = {
      enabled: true,
      domain: 'example.com',
      email: 'admin@example.com',
      acceptTOS: true,
      staging: false,
      certCache: 'certs',
    };
    mockFetch({ ok: true });

    const result = await tlsApi.save(payload);
    const [url, options] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/admin/tls-config');
    expect(options.method).toBe('PUT');
    expect(JSON.parse(options.body as string)).toEqual(payload);
    expect(result).toEqual({ ok: true });
  });

  it('publicIp fetches /api/admin/tls-config/public-ip', async () => {
    mockFetch({ data: { ip: '1.2.3.4' } });

    const result = await tlsApi.publicIp();
    expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0]).toBe(
      '/api/admin/tls-config/public-ip',
    );
    expect(result).toEqual({ data: { ip: '1.2.3.4' } });
  });

  it('checkDns fetches /api/admin/tls-config/check-dns with the domain', async () => {
    mockFetch({
      data: {
        domain: 'example.com',
        publicIP: '1.2.3.4',
        resolvedIP: '1.2.3.4',
        matches: true,
      },
    });

    const result = await tlsApi.checkDns('example.com');
    expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0]).toBe(
      '/api/admin/tls-config/check-dns?domain=example.com',
    );
    expect(result).toEqual({
      data: {
        domain: 'example.com',
        publicIP: '1.2.3.4',
        resolvedIP: '1.2.3.4',
        matches: true,
      },
    });
  });
});
