import { existsSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));

function getBinaryName() {
  return process.platform === 'win32' ? 'axonrouter.exe' : 'axonrouter';
}

function getBinPath() {
  return join(__dirname, 'bin', getBinaryName());
}

export const binPath = getBinPath();

export function isInstalled() {
  return existsSync(binPath);
}
