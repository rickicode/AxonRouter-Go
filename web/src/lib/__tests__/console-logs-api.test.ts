import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { getConsoleLogs, streamConsoleLogs, clearConsoleLogs } from '$lib/api';

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

describe('streamConsoleLogs', () => {
	let createdInstances: MockEventSource[] = [];

	class MockEventSource {
		url: string;
		onopen: (() => void) | null = null;
		onerror: (() => void) | null = null;

		constructor(url: string) {
			this.url = url;
			createdInstances.push(this);
		}

		addEventListener() {}
		close() {}
	}

	beforeEach(() => {
		createdInstances = [];
		vi.stubGlobal('EventSource', MockEventSource);
		globalThis.localStorage.setItem('axon_token', 'test-token');
	});

	afterEach(() => {
		createdInstances = [];
		vi.unstubAllGlobals();
	});

	it('creates an EventSource pointing to /api/admin/console-logs/stream', () => {
		const es = streamConsoleLogs();
		expect(es).toBeInstanceOf(MockEventSource);
		expect(es.url).toBe('/api/admin/console-logs/stream?token=test-token');
	});

	it('passes level, search and token as query params', () => {
		streamConsoleLogs({ level: 'error', search: 'timeout' });
		const es = createdInstances[0];
		expect(es.url).toContain('/api/admin/console-logs/stream');
		expect(es.url).toContain('level=error');
		expect(es.url).toContain('search=timeout');
		expect(es.url).toContain('token=test-token');
	});

	it('omits empty filter params from the query string', () => {
		streamConsoleLogs({ level: 'info' });
		const es = createdInstances[0];
		expect(es.url).toContain('level=info');
		expect(es.url).not.toContain('search=');
	});
});

describe('clearConsoleLogs', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('calls DELETE /api/admin/console-logs', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({}),
		});
		vi.stubGlobal('fetch', fetchMock);
		await clearConsoleLogs();
		expect(fetchMock).toHaveBeenCalledTimes(1);
		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe('/api/admin/console-logs');
		expect(calls[0][1].method).toBe('DELETE');
	});
});
