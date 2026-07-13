import { describe, it, expect, beforeEach, vi } from 'vitest';
import { getToken, setToken, logout, getMustChangePassword, setMustChangePassword } from '$lib/auth';
import { passwordApi } from '$lib/api';

const TOKEN_KEY = 'axon_token';
const MUST_CHANGE_KEY = 'axon_must_change_password';

describe('auth storage', () => {
	beforeEach(() => {
		sessionStorage.clear();
	});

	it('round-trips the token', () => {
		expect(getToken()).toBeNull();
		setToken('abc123');
		expect(getToken()).toBe('abc123');
	});

	it('clears both token and must-change flag on logout', () => {
		setToken('abc123');
		setMustChangePassword(true);
		logout();
		expect(getToken()).toBeNull();
		expect(getMustChangePassword()).toBe(false);
	});
});

describe('passwordApi', () => {
	beforeEach(() => {
		sessionStorage.clear();
		vi.restoreAllMocks();
	});

	it('change sends old/new/confirm password', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({ ok: true }),
		} as unknown as Response);
		vi.stubGlobal('fetch', fetchMock);

		setToken('token123');
		await passwordApi.change('oldpass', 'newpass', 'newpass');

		const [, init] = fetchMock.mock.calls[0];
		const body = JSON.parse((init as RequestInit).body as string);
		expect(body).toEqual({
			old_password: 'oldpass',
			new_password: 'newpass',
			confirm_password: 'newpass',
		});
		const headers = (init as RequestInit).headers as Record<string, string>;
		expect(headers.Authorization).toBe('Bearer token123');
	});

	it('deferChange sends an authenticated POST', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: { get: () => null },
			json: () => Promise.resolve({ password_change_due_at: 1234567890 }),
		} as unknown as Response);
		vi.stubGlobal('fetch', fetchMock);

		setToken('token123');
		await passwordApi.deferChange();

		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toContain('/defer-password-change');
		expect((init as RequestInit).method).toBe('POST');
	});
});
