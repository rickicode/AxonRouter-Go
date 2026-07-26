import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';

const source = readFileSync('./src/pages/Console.svelte', 'utf-8');

describe('Console.svelte SSE and clear logs', () => {
	it('imports the stream and clear helpers', () => {
		expect(source).toContain('streamConsoleLogs');
		expect(source).toContain('clearConsoleLogs');
	});

	it('connects via EventSource first and defines SSE lifecycle helpers', () => {
		expect(source).toContain('function startSSE');
		expect(source).toContain('function closeSSE');
		expect(source).toContain('streamConsoleLogs(');
		expect(source).toContain('EventSource');
	});

	it('handles init, line and clear SSE event types', () => {
		expect(source).toContain("addEventListener('init'");
		expect(source).toContain("addEventListener('line'");
		expect(source).toContain("addEventListener('clear'");
		expect(source).toContain("applySSEEvent('init'");
		expect(source).toContain("applySSEEvent('line'");
		expect(source).toContain("applySSEEvent('clear'");
	});

	it('falls back to polling when SSE is unavailable or errors', () => {
		expect(source).toContain('typeof EventSource === \'undefined\'');
		expect(source).toContain('startPolling()');
		expect(source).toContain('es.onerror');
	});

	it('renders a Clear Logs button with a confirmation dialog', () => {
		expect(source).toContain('Clear Logs');
		expect(source).toContain('window.confirm');
		expect(source).toContain('handleClearLogs');
	});

	it('calls clearConsoleLogs to clear backend logs and the local UI', () => {
		expect(source).toContain('await clearConsoleLogs()');
		expect(source).toContain('logs.entries = []');
	});
});
