import { describe, it, expect, vi, beforeEach } from 'vitest';
import { getConsoleLogs } from '$lib/api';

describe('getConsoleLogs', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('fetches structured entries from the console-logs endpoint', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () =>
				Promise.resolve({
					entries: [
						{ ts: '2026-01-01T00:00:00Z', level: 'info', msg: 'server started' },
						{ ts: '2026-01-01T00:00:01Z', level: 'error', msg: 'connection failed', error: 'timeout' },
					],
					path: '/tmp/axonrouter.log',
					total: 2,
				}),
		});
		vi.stubGlobal('fetch', fetchMock);

		const result = await getConsoleLogs();

		expect(result.entries).toHaveLength(2);
		expect(result.entries[0].level).toBe('info');
		expect(result.entries[1].level).toBe('error');
		expect(result.path).toBe('/tmp/axonrouter.log');
		expect(result.total).toBe(2);

		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe('/api/admin/console-logs');
		expect(calls[0][1].method ?? 'GET').toBe('GET');
	});

	it('passes level and search params as query string', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({ entries: [], path: '/tmp/axonrouter.log', total: 0 }),
		});
		vi.stubGlobal('fetch', fetchMock);

		await getConsoleLogs({ level: 'warn', search: 'timeout' });

		const calls = fetchMock.mock.calls as [string, RequestInit][];
		const url = calls[0][0];
		expect(url).toContain('level=warn');
		expect(url).toContain('search=timeout');
	});

	it('omits empty params from query string', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({ entries: [], path: '/tmp/axonrouter.log', total: 0 }),
		});
		vi.stubGlobal('fetch', fetchMock);

		await getConsoleLogs({ level: 'debug' });

		const calls = fetchMock.mock.calls as [string, RequestInit][];
		const url = calls[0][0];
		expect(url).not.toContain('search=');
	});
});
