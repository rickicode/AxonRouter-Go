import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';

const source = readFileSync('./src/pages/About.svelte', 'utf-8');

describe('About upgrade logs and restart prompt', () => {
  it('captures upgrade logs from the backend response', () => {
    expect(source).toContain('let upgradeLogs = $state<string[]>([])');
    expect(source).toContain("const logs = Array.isArray(data.logs) ? data.logs : []");
    expect(source).toContain('upgradeLogs = logs');
  });

  it('renders the upgrade logs after success', () => {
    expect(source).toContain('{#each upgradeLogs');
    expect(source).toContain('Upgrade logs');
  });

  it('calls the restart endpoint after user confirms', () => {
    expect(source).toContain("fetch('/api/admin/restart'");
    expect(source).toContain('method: \'POST\'');
    expect(source).toContain('async function handleRestart');
  });

  it('shows a restart confirmation prompt on upgrade success', () => {
    expect(source).toContain('Restart the axonrouter service now?');
    expect(source).toMatch(/confirmRestart|handleRestart/);
  });

  it('uses restart command from backend fallback when restart fails', () => {
    expect(source).toContain("typeof data.restart_command === 'string' ? data.restart_command : restartCommand");
  });
});
