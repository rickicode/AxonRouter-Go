const DISMISSED_KEY = 'axon_update_modal_dismissed';

export function isUpdateModalDismissed(): boolean {
	try {
		return sessionStorage.getItem(DISMISSED_KEY) === 'true';
	} catch {
		return false;
	}
}

export function dismissUpdateModal(): void {
	try {
		sessionStorage.setItem(DISMISSED_KEY, 'true');
	} catch {
		// ignore storage restrictions
	}
}
