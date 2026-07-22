import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiKeysApi } from '$lib/api';

describe('apiKeysApi.create', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('sends allowed_models in the request body', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () =>
				Promise.resolve({
					id: 'key-1',
					key: 'secret',
					name: 'test',
					max_tokens: 1000,
					message: 'ok',
				}),
		});
		vi.stubGlobal('fetch', fetchMock);

		await apiKeysApi.create('test', 60, 1000, undefined, ['gpt-4o', 'claude-sonnet']);

		expect(fetchMock).toHaveBeenCalledTimes(1);
		const init = fetchMock.mock.calls[0][1] as RequestInit;
		const body = JSON.parse(init.body as string);
		expect(body.allowed_models).toEqual(['gpt-4o', 'claude-sonnet']);
	});

	it('omits allowed_models from the body when not provided', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () =>
				Promise.resolve({
					id: 'key-2',
					key: 'secret',
					name: 'test',
					max_tokens: 1000,
					message: 'ok',
				}),
		});
		vi.stubGlobal('fetch', fetchMock);

		await apiKeysApi.create('test', 60, 1000);

		const init = fetchMock.mock.calls[0][1] as RequestInit;
		const body = JSON.parse(init.body as string);
		expect(body.allowed_models).toBeUndefined();
	});
});
