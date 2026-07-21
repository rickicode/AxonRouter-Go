import { describe, it, expect, vi, beforeEach } from 'vitest';
import { usageApi, type UsageActivityDay, type UsageActivityResponse } from '$lib/api';

describe('usageApi.activity', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('fetches /api/admin/usage/activity with filters', async () => {
		const day: UsageActivityDay = {
			date: '2026-07-21',
			requests: 10,
			tokens: 1234,
			cost_usd: 0.05,
		};
		const payload: UsageActivityResponse = {
			data: {
				from: '2026-07-01',
				to: '2026-07-21',
				days: [day],
			},
		};

		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve(payload),
		});
		vi.stubGlobal('fetch', fetchMock);

		const result = await usageApi.activity({
			api_key_id: 'key1',
			provider_id: 'openai',
			model_id: 'gpt-4o',
			modality: 'text',
			status_code: 200,
		});

		expect(result).toEqual(payload);
		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe(
			'/api/admin/usage/activity?api_key_id=key1&provider_id=openai&model_id=gpt-4o&modality=text&status_code=200',
		);
	});

	it('fetches /api/admin/usage/activity without params', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () =>
				Promise.resolve({
					data: {
						from: '2026-07-01',
						to: '2026-07-21',
						days: [],
					},
				}),
		});
		vi.stubGlobal('fetch', fetchMock);

		await usageApi.activity();

		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe('/api/admin/usage/activity');
	});
});
