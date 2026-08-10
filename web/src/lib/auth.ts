import { writable } from 'svelte/store';

const TOKEN_KEY = "axon_token";
const MUST_CHANGE_PASSWORD_KEY = "axon_must_change_password";
const PASSWORD_WARNING_DISMISSED_KEY = "axon_password_warning_dismissed";
const PASSWORD_WARNING_DISMISSED_AT_KEY = "axon_password_warning_dismissed_at";
const REMEMBER_KEY = "axon_remember_me";
const PASSWORD_WARNING_DISMISSAL_TTL_MS = 24 * 60 * 60 * 1000;

function useLongTermStorage(explicit?: boolean): boolean {
  return explicit ?? localStorage.getItem(REMEMBER_KEY) === "true";
}

function setInPreferredStorage(key: string, value: string, remember?: boolean) {
  if (useLongTermStorage(remember)) {
    localStorage.setItem(key, value);
    sessionStorage.removeItem(key);
  } else {
    sessionStorage.setItem(key, value);
    localStorage.removeItem(key);
  }
}

function removeFromBoth(key: string) {
  localStorage.removeItem(key);
  sessionStorage.removeItem(key);
}

export function getRememberMe(): boolean {
  return localStorage.getItem(REMEMBER_KEY) === "true";
}

export function setRememberMe(v: boolean) {
  localStorage.setItem(REMEMBER_KEY, v ? "true" : "false");
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY) || sessionStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string | null, remember?: boolean) {
  if (!t) {
    removeFromBoth(TOKEN_KEY);
    return;
  }
  setInPreferredStorage(TOKEN_KEY, t, remember);
}

export function getMustChangePassword(): boolean {
  return localStorage.getItem(MUST_CHANGE_PASSWORD_KEY) === "true" ||
         sessionStorage.getItem(MUST_CHANGE_PASSWORD_KEY) === "true";
}

export function setMustChangePassword(v: boolean, remember?: boolean) {
  setInPreferredStorage(MUST_CHANGE_PASSWORD_KEY, v ? "true" : "false", remember);
  mustChangePasswordStore.set(v);
}

export const authStore = writable<boolean>(!!getToken());
export const mustChangePasswordStore = writable<boolean>(getMustChangePassword());

export function isPasswordWarningDismissed(): boolean {
  // Migrate the old boolean flag: if it exists, treat it as expired so the
  // warning is shown once more and then converted to the new timestamp format.
  const legacy =
    localStorage.getItem(PASSWORD_WARNING_DISMISSED_KEY) === "true" ||
    sessionStorage.getItem(PASSWORD_WARNING_DISMISSED_KEY) === "true";
  if (legacy) {
    removeFromBoth(PASSWORD_WARNING_DISMISSED_KEY);
    return false;
  }

  const at =
    localStorage.getItem(PASSWORD_WARNING_DISMISSED_AT_KEY) ||
    sessionStorage.getItem(PASSWORD_WARNING_DISMISSED_AT_KEY);
  if (!at) return false;
  const dismissedAt = parseInt(at, 10);
  if (Number.isNaN(dismissedAt)) return false;
  return Date.now() - dismissedAt < PASSWORD_WARNING_DISMISSAL_TTL_MS;
}

export function dismissPasswordWarning(remember?: boolean) {
  setInPreferredStorage(PASSWORD_WARNING_DISMISSED_AT_KEY, String(Date.now()), remember);
}

export function clearPasswordWarningDismissal() {
  removeFromBoth(PASSWORD_WARNING_DISMISSED_KEY);
  removeFromBoth(PASSWORD_WARNING_DISMISSED_AT_KEY);
}

export function logout() {
  setToken(null);
  setMustChangePassword(false);
  clearPasswordWarningDismissal();
  setRememberMe(false);
  authStore.set(false);
}
