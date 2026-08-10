export type LimitMode = 'none' | 'providers' | 'models';

export function payloadFromSelections(
	mode: LimitMode,
	providers: string[],
	models: string[],
): string[] | undefined {
	if (mode === 'providers') return providers;
	if (mode === 'models') return models;
	return undefined;
}

export function formatAllowlistSummary(mode: LimitMode, allowed?: string[]): string {
	if (!allowed || allowed.length === 0) return 'Unlimited';
	const count = allowed.length;
	if (mode === 'providers') return `Limited to ${count} provider${count === 1 ? '' : 's'}`;
	if (mode === 'models') return `Limited to ${count} model${count === 1 ? '' : 's'}`;
	return `Limited to ${count} item${count === 1 ? '' : 's'}`;
}

export function detectLimitMode(allowed?: string[]): LimitMode {
	if (!allowed || allowed.length === 0) return 'none';
	if (allowed.every((id) => id.includes('/'))) return 'models';
	return 'providers';
}
