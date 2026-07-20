import { writable } from 'svelte/store';
import { clearPasswordWarningDismissal, setMustChangePassword } from './auth';

export const healthOnline = writable(true);
export const healthLatencyMs = writable(1);
export const healthCurrentVersion = writable<string>('');
export const healthLatestVersion = writable<string>('');
export const healthUpdateAvailable = writable<boolean>(false);

const INTERVAL_MS = 30000;
let interval: ReturnType<typeof setInterval> | undefined;

function applyHealthData(data: Record<string, unknown>) {
	healthCurrentVersion.set(typeof data.version === 'string' ? data.version : '');
	healthLatestVersion.set(typeof data.latest_version === 'string' ? data.latest_version : '');
	healthUpdateAvailable.set(data.update_available === true);

	if (typeof data.must_change_password === 'boolean') {
		if (data.must_change_password) {
			setMustChangePassword(true);
		} else {
			clearPasswordWarningDismissal();
			setMustChangePassword(false);
		}
	}
}

async function check() {
	const start = performance.now();
	try {
		const res = await fetch('/api/admin/health');
		const latency = Math.max(1, Math.round(performance.now() - start));
		healthLatencyMs.set(latency);
		if (res.ok) {
			const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
			healthOnline.set(true);
			applyHealthData(data);
		} else {
			healthOnline.set(false);
		}
	} catch {
		healthOnline.set(false);
		healthLatencyMs.set(0);
	}
}

export function startHealthChecks() {
	void check();
	interval = setInterval(check, INTERVAL_MS);
	return () => {
		if (interval) {
			clearInterval(interval);
			interval = undefined;
		}
	};
}
