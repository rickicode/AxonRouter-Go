import { describe, it, expect } from 'vitest';
import {
	payloadFromSelections,
	formatAllowlistSummary,
	detectLimitMode,
	type LimitMode,
} from '$lib/api-key-allowlist';

describe('payloadFromSelections', () => {
	it('returns provider IDs for providers mode', () => {
		expect(payloadFromSelections('providers', ['openai', 'claude'], ['openai/gpt-4o'])).toEqual([
			'openai',
			'claude',
		]);
	});

	it('returns model IDs for models mode', () => {
		expect(payloadFromSelections('models', ['openai'], ['openai/gpt-4o', 'claude/sonnet'])).toEqual(
			['openai/gpt-4o', 'claude/sonnet'],
		);
	});

	it('returns undefined for none mode', () => {
		expect(payloadFromSelections('none', ['openai'], ['openai/gpt-4o'])).toBeUndefined();
	});

	it('returns empty array as-is so validation can reject it', () => {
		expect(payloadFromSelections('providers', [], [])).toEqual([]);
		expect(payloadFromSelections('models', [], [])).toEqual([]);
	});
});

describe('formatAllowlistSummary', () => {
	it('reports unlimited when no allowlist', () => {
		expect(formatAllowlistSummary('none')).toBe('Unlimited');
		expect(formatAllowlistSummary('providers', [])).toBe('Unlimited');
		expect(formatAllowlistSummary('models', [])).toBe('Unlimited');
	});

	it('formats provider limit singular and plural', () => {
		expect(formatAllowlistSummary('providers', ['openai'])).toBe('Limited to 1 provider');
		expect(formatAllowlistSummary('providers', ['openai', 'claude'])).toBe(
			'Limited to 2 providers',
		);
	});

	it('formats model limit singular and plural', () => {
		expect(formatAllowlistSummary('models', ['openai/gpt-4o'])).toBe('Limited to 1 model');
		expect(formatAllowlistSummary('models', ['openai/gpt-4o', 'claude/sonnet'])).toBe(
			'Limited to 2 models',
		);
	});
});

describe('detectLimitMode', () => {
	it('detects none for missing or empty allowlist', () => {
		expect(detectLimitMode(undefined)).toBe('none');
		expect(detectLimitMode([])).toBe('none');
	});

	it('detects providers when no entry contains a slash', () => {
		expect(detectLimitMode(['openai', 'claude', 'kiro'])).toBe('providers');
	});

	it('detects models when every entry contains a slash', () => {
		expect(detectLimitMode(['openai/gpt-4o', 'claude/sonnet'])).toBe('models');
	});
});
