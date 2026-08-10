export const KIRO_METHODS = [
	{ id: 'builder-id', label: 'AWS Builder ID' },
	{ id: 'idc', label: 'AWS IAM Identity Center' },
	{ id: 'api-key', label: 'API Key' },
	{ id: 'google', label: 'Google' },
	{ id: 'github', label: 'GitHub' },
	{ id: 'import', label: 'Import from CLI / Token' },
] as const;

export type KiroMethod = (typeof KIRO_METHODS)[number]['id'];

export type KiroScreen = KiroMethod | 'menu';

export const KIRO_STARTING_METHOD: KiroScreen = 'menu';

export function getKiroMethodLabel(method: KiroMethod): string {
	return KIRO_METHODS.find((m) => m.id === method)?.label ?? method;
}
