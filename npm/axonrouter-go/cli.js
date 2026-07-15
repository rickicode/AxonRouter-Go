#!/usr/bin/env node
import { spawnSync } from 'child_process';
import { existsSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const binName = process.platform === 'win32' ? 'axonrouter.exe' : 'axonrouter';
const binPath = join(__dirname, 'bin', binName);

if (!existsSync(binPath)) {
  console.error('AxonRouter-Go binary is not installed. Run: npm run postinstall');
  process.exit(1);
}

const result = spawnSync(binPath, process.argv.slice(2), {
  stdio: 'inherit',
  shell: false,
});

process.exit(result.status ?? 1);
