import { describe, it, expect, vi, beforeEach } from 'vitest';
import { getConsoleLogs } from '$lib/api';

describe('getConsoleLogs', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('fetches lines and path from the console-logs endpoint', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () =>
				Promise.resolve({
					lines: ['line one', 'line two'],
					path: '/tmp/axonrouter.log',
				}),
		});
		vi.stubGlobal('fetch', fetchMock);

		const result = await getConsoleLogs();

		expect(result).toEqual({
			lines: ['line one', 'line two'],
			path: '/tmp/axonrouter.log',
		});
		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe('/api/admin/console-logs');
		expect(calls[0][1].method ?? 'GET').toBe('GET');
	});
});
