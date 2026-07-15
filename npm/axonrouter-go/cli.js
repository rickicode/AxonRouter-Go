#!/usr/bin/env node
import { spawnSync } from 'child_process';
import { existsSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const binName = process.platform === 'win32' ? 'axonrouter.exe' : 'axonrouter';
const binPath = join(__dirname, 'bin', binName);

if (!existsSync(binPath)) {
  console.error('AxonRouter-Go binary is not installed. Re-run the install script:');
  console.error('  cd node_modules/axonrouter-go && node install.js');
  process.exit(1);
}

const result = spawnSync(binPath, process.argv.slice(2), {
  stdio: 'inherit',
  shell: false,
});

if (result.signal) {
  process.kill(process.pid, result.signal);
} else {
  process.exit(result.status ?? 1);
}
