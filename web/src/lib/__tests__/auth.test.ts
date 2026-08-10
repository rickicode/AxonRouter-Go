import { describe, it, expect, beforeEach, vi } from 'vitest';
import { getToken, setToken, logout, getMustChangePassword, setMustChangePassword, isPasswordWarningDismissed, dismissPasswordWarning, clearPasswordWarningDismissal } from '$lib/auth';
import { passwordApi } from '$lib/api';

const TOKEN_KEY = 'axon_token';
const MUST_CHANGE_KEY = 'axon_must_change_password';

describe('auth storage', () => {
	beforeEach(() => {
		localStorage.clear();
		sessionStorage.clear();
	});

	it('round-trips the token to session storage by default', () => {
		expect(getToken()).toBeNull();
		setToken('abc123');
		expect(getToken()).toBe('abc123');
		expect(sessionStorage.getItem(TOKEN_KEY)).toBe('abc123');
		expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
	});

	it('persists the token in localStorage when remembered', () => {
		setToken('abc123', true);
		expect(getToken()).toBe('abc123');
		expect(localStorage.getItem(TOKEN_KEY)).toBe('abc123');
		expect(sessionStorage.getItem(TOKEN_KEY)).toBeNull();
	});

	it('clears both token and must-change flag on logout', () => {
		setToken('abc123', true);
		setMustChangePassword(true, true);
		logout();
		expect(getToken()).toBeNull();
		expect(getMustChangePassword()).toBe(false);
	});

	it('remembers password warning dismissal for 24 hours', () => {
		expect(isPasswordWarningDismissed()).toBe(false);
		dismissPasswordWarning(true);
		expect(isPasswordWarningDismissed()).toBe(true);
	});

	it('shows the password warning again after 24 hours', () => {
		const overOneDayAgo = String(Date.now() - 25 * 60 * 60 * 1000);
		localStorage.setItem('axon_password_warning_dismissed_at', overOneDayAgo);
		expect(isPasswordWarningDismissed()).toBe(false);
	});

	it('clears password warning dismissal on logout', () => {
		dismissPasswordWarning(true);
		setToken('abc123', true);
		logout();
		expect(isPasswordWarningDismissed()).toBe(false);
	});
});

describe('passwordApi', () => {
	beforeEach(() => {
		localStorage.clear();
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
