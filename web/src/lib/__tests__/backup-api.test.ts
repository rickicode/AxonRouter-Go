import { describe, it, expect, vi, beforeEach } from 'vitest';
import { backupApi } from '$lib/api';

describe('backupApi', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('downloadBackup posts selected categories and password as JSON body', async () => {
    const blob = new Blob(['backup'], { type: 'application/x-ndjson' });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        blob: () => Promise.resolve(blob),
      }),
    );

    const result = await backupApi.downloadBackup({
      categories: ['providers', 'config'],
      password: 'secret',
    });

    const [url, options] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/admin/backup/download');
    expect(options.method).toBe('POST');
    expect(options.body).toBe(JSON.stringify({ categories: ['providers', 'config'], password: 'secret' }));
    expect(result).toBe(blob);
  });

  it('restoreBackup posts multipart form data with file, target, and password', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: () => Promise.resolve({ restored: true }),
      }),
    );
    const file = new File(['backup'], 'backup.ndjson', { type: 'application/x-ndjson' });

    const result = await backupApi.restoreBackup({
      file,
      target: 'sqlite',
      password: 'secret',
    });

    const [url, options] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/admin/backup/restore');
    expect(options.method).toBe('POST');
    expect(options.headers['Content-Type']).toBeUndefined();
    expect(options.body).toBeInstanceOf(FormData);
    const body = options.body as FormData;
    expect(body.get('backup')).toBe(file);
    expect(body.get('target')).toBe('sqlite');
    expect(body.get('password')).toBe('secret');
    expect(result).toEqual({ restored: true });
  });
});
