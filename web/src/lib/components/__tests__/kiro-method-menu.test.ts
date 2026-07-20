import { describe, it, expect } from 'vitest';
import { KIRO_METHODS, KIRO_STARTING_METHOD, getKiroMethodLabel } from '../kiro-method-menu';

describe('kiro-method-menu', () => {
	it('exposes six methods in the required order', () => {
		expect(KIRO_METHODS.map((m) => m.label)).toEqual([
			'AWS Builder ID',
			'AWS IAM Identity Center',
			'API Key',
			'Google',
			'GitHub',
			'Import from CLI / Token',
		]);
	});

	it('starts at the menu placeholder', () => {
		expect(KIRO_STARTING_METHOD).toBe('menu');
	});

	it('resolves labels for each method', () => {
		for (const method of KIRO_METHODS) {
			expect(getKiroMethodLabel(method.id)).toBe(method.label);
		}
	});
});
