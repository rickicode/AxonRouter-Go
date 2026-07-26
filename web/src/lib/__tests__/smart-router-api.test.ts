import { describe, it, expect } from 'vitest';
import {
	SMART_VIRTUAL_MODEL_IDS,
	defaultSmartRouterConfig,
	computeVirtualModelTelemetry,
	type VirtualModelTelemetry,
} from '$lib/api';

describe('SMART_VIRTUAL_MODEL_IDS', () => {
	it('contains the required smart virtual model names', () => {
		expect(SMART_VIRTUAL_MODEL_IDS).toEqual([
			'smart/auto',
			'smart/auto-fast',
			'smart/auto-quality',
		]);
	});
});

describe('defaultSmartRouterConfig', () => {
	it('enables every virtual model by default with empty candidates', () => {
		expect(defaultSmartRouterConfig.models).toHaveLength(3);
		for (const m of defaultSmartRouterConfig.models) {
			expect(m.enabled).toBe(true);
			expect(m.candidates).toEqual([]);
			expect(SMART_VIRTUAL_MODEL_IDS).toContain(m.id);
		}
	});
});

describe('computeVirtualModelTelemetry', () => {
	const rows = [
		{
			model_id: 'openai/gpt-4o',
			requests: 100,
			errors: 5,
			avg_latency_ms: 200,
			cost_usd: 0.5,
			total_tokens: 100000,
		},
		{
			model_id: 'claude/sonnet',
			requests: 50,
			errors: 0,
			avg_latency_ms: 300,
			cost_usd: 0.6,
			total_tokens: 40000,
		},
		{
			model_id: 'deepseek/chat',
			requests: 10,
			errors: 1,
			avg_latency_ms: 500,
			cost_usd: 0.02,
			total_tokens: 10000,
		},
	];

	it('returns null when no candidates match usage rows', () => {
		expect(computeVirtualModelTelemetry(['unknown/model'], rows)).toBeNull();
	});

	it('returns null when no candidates are supplied', () => {
		expect(computeVirtualModelTelemetry([], rows)).toBeNull();
	});

	it('computes weighted telemetry across candidates', () => {
		const result = computeVirtualModelTelemetry(
			['openai/gpt-4o', 'claude/sonnet'],
			rows,
		) as VirtualModelTelemetry;

		expect(result.requests).toBe(150);
		// weighted latency = (100*200 + 50*300) / 150
		expect(result.avgLatencyMs).toBeCloseTo(233.33, 1);
		// success rate = (150 - 5) / 150
		expect(result.successRate).toBeCloseTo(96.67, 1);
		// cost per 1K tokens = ((0.5 + 0.6) / (100000 + 40000)) * 1000
		expect(result.costPer1KTokens).toBeCloseTo(0.007857, 4);
	});

	it('handles zero-token edge case without crashing', () => {
		const result = computeVirtualModelTelemetry(
			['zero/tokens'],
			[
				{
					model_id: 'zero/tokens',
					requests: 1,
					errors: 0,
					avg_latency_ms: 100,
					cost_usd: 0.01,
					total_tokens: 0,
				},
			],
		) as VirtualModelTelemetry;
		expect(result.requests).toBe(1);
		expect(result.costPer1KTokens).toBe(0);
	});
});
