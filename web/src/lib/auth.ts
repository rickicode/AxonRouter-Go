import { writable } from 'svelte/store';

const TOKEN_KEY = "axon_token";
const MUST_CHANGE_PASSWORD_KEY = "axon_must_change_password";
const PASSWORD_WARNING_DISMISSED_KEY = "axon_password_warning_dismissed";

export function getToken(): string | null {
  return sessionStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string | null) {
  if (t) sessionStorage.setItem(TOKEN_KEY, t);
  else sessionStorage.removeItem(TOKEN_KEY);
}

export function getMustChangePassword(): boolean {
  return sessionStorage.getItem(MUST_CHANGE_PASSWORD_KEY) === "true";
}

export function setMustChangePassword(v: boolean) {
  sessionStorage.setItem(MUST_CHANGE_PASSWORD_KEY, v ? "true" : "false");
  mustChangePasswordStore.set(v);
}

export const authStore = writable<boolean>(!!getToken());
export const mustChangePasswordStore = writable<boolean>(getMustChangePassword());

export function isPasswordWarningDismissed(): boolean {
	return sessionStorage.getItem(PASSWORD_WARNING_DISMISSED_KEY) === "true";
}

export function dismissPasswordWarning() {
	sessionStorage.setItem(PASSWORD_WARNING_DISMISSED_KEY, "true");
}

export function clearPasswordWarningDismissal() {
	sessionStorage.removeItem(PASSWORD_WARNING_DISMISSED_KEY);
}

export function logout() {
	setToken(null);
	setMustChangePassword(false);
	clearPasswordWarningDismissal();
	authStore.set(false);
}
