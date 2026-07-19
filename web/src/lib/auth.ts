import { writable } from 'svelte/store';

const TOKEN_KEY = "axon_token";
const MUST_CHANGE_PASSWORD_KEY = "axon_must_change_password";
const PASSWORD_WARNING_DISMISSED_KEY = "axon_password_warning_dismissed";
const REMEMBER_KEY = "axon_remember_me";

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
  return localStorage.getItem(PASSWORD_WARNING_DISMISSED_KEY) === "true" ||
         sessionStorage.getItem(PASSWORD_WARNING_DISMISSED_KEY) === "true";
}

export function dismissPasswordWarning(remember?: boolean) {
  setInPreferredStorage(PASSWORD_WARNING_DISMISSED_KEY, "true", remember);
}

export function clearPasswordWarningDismissal() {
  removeFromBoth(PASSWORD_WARNING_DISMISSED_KEY);
}

export function logout() {
  setToken(null);
  setMustChangePassword(false);
  clearPasswordWarningDismissal();
  setRememberMe(false);
  authStore.set(false);
}
