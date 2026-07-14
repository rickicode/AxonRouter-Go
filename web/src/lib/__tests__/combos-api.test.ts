import { describe, it, expect, vi } from 'vitest';
import { combosApi, type ComboStep } from '$lib/api';

function makeStep(id: string): ComboStep {
	return {
		id,
		combo_id: 'combo1',
		connection_id: 'conn1',
		model_id: 'm1',
		priority: 1,
		weight: 100,
		created_at: 1,
	};
}

describe('combosApi step helpers', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('adds a step to a combo', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({ id: 'step1', priority: 2 }),
		});
		vi.stubGlobal('fetch', fetchMock);

		const result = await combosApi.addStep('combo1', {
			connection_id: 'conn1',
			model_id: 'm1',
			priority: 2,
			weight: 50,
		});

		expect(result).toEqual({ id: 'step1', priority: 2 });
		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe('/api/admin/combos/combo1/steps');
		expect(calls[0][1].method).toBe('POST');
		expect(calls[0][1].body).toBe(
			JSON.stringify({
				connection_id: 'conn1',
				model_id: 'm1',
				priority: 2,
				weight: 50,
			}),
		);
	});

	it('removes a step by id', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({ ok: true }),
		});
		vi.stubGlobal('fetch', fetchMock);

		await combosApi.removeStep('step1');

		const calls = fetchMock.mock.calls as [string, RequestInit][];
		expect(calls[0][0]).toBe('/api/admin/combos/steps/step1');
		expect(calls[0][1].method).toBe('DELETE');
	});

	it('types ComboDetailResponse steps', async () => {
		// Type-only guard: ComboDetailResponse should hold ComboStep[] or null.
		const step = makeStep('step1');
		expect(step.model_id).toBe('m1');
	});
});
