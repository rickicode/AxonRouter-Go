import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';

describe('CLI Tools list + detail pages', () => {
	it('renders tool cards on the list page', () => {
		const source = readFileSync('./src/pages/CLIToolsList.svelte', 'utf-8');
		expect(source).toContain('CLIToolCard');
		expect(source).toContain('/cli-tools/');
		expect(source).not.toContain('Dialog.Root');
	});

	it('has a detail page that loads by id and navigates back', () => {
		const source = readFileSync('./src/pages/CLIToolDetail.svelte', 'utf-8');
		expect(source).toContain('cliToolsApi.get(id)');
		expect(source).toContain("router.navigate('/cli-tools')");
		expect(source).toContain('CLIConfigOutput');
	});

	it('routes /cli-tools and /cli-tools/:id in App.svelte', () => {
		const source = readFileSync('./src/App.svelte', 'utf-8');
		expect(source).toContain('CLIToolsList');
		expect(source).toContain('CLIToolDetail');
		expect(source).toContain("segments[0] === 'cli-tools' && segments.length === 2");
	});
});
