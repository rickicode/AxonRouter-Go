import { writable } from 'svelte/store';

const TOKEN_KEY = "axon_token";

export function getToken(): string | null {
	return sessionStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string | null) {
	if (t) sessionStorage.setItem(TOKEN_KEY, t);
	else sessionStorage.removeItem(TOKEN_KEY);
}

export const authStore = writable<boolean>(!!getToken());

export function logout() {
	setToken(null);
	authStore.set(false);
}
