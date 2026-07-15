#!/usr/bin/env node
/**
 * Downloads the AxonRouter-Go binary for the current platform during npm install.
 * Verifies the SHA256 checksum against the release's checksums.txt.
 */

import { createWriteStream, existsSync, mkdirSync, readFileSync, writeFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';
import { createHash } from 'crypto';
import https from 'https';
import http from 'http';
import { execSync } from 'child_process';

const __dirname = dirname(fileURLToPath(import.meta.url));
const pkg = JSON.parse(readFileSync(join(__dirname, 'package.json'), 'utf8'));

const REPO = 'rickicode/AxonRouter-Go';
const VERSION = process.env.AXONROUTER_VERSION || pkg.version;

function info(msg) {
  console.log(`[axonrouter-go] ${msg}`);
}

function error(msg) {
  console.error(`[axonrouter-go] error: ${msg}`);
}

function platformToGoos(platform) {
  switch (platform) {
    case 'win32': return 'windows';
    case 'darwin': return 'darwin';
    case 'linux': return 'linux';
    case 'freebsd': return 'freebsd';
    default:
      throw new Error(`unsupported platform: ${platform}`);
  }
}

function archToGoarch(arch) {
  switch (arch) {
    case 'x64': return 'amd64';
    case 'arm64': return 'arm64';
    case 'ia32': return '386';
    case 'arm': return 'arm';
    default:
      throw new Error(`unsupported architecture: ${arch}`);
  }
}

function getAssetName() {
  const goos = platformToGoos(process.platform);
  const goarch = archToGoarch(process.arch);
  const ext = goos === 'windows' ? '.exe' : '';
  return `axonrouter-${goos}-${goarch}${ext}`;
}

function getBinaryName() {
  return process.platform === 'win32' ? 'axonrouter.exe' : 'axonrouter';
}

function download(url) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith('https:') ? https : http;
    const req = client.get(url, { headers: { 'User-Agent': `axonrouter-go-npm/${VERSION}` } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return download(new URL(res.headers.location, url).toString()).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
      }
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve(Buffer.concat(chunks)));
    });
    req.on('error', reject);
    req.setTimeout(120000, () => {
      req.destroy();
      reject(new Error(`download timeout for ${url}`));
    });
  });
}

async function main() {
  if (process.env.SKIP_AXONROUTER_DOWNLOAD === 'true') {
    info('SKIP_AXONROUTER_DOWNLOAD=true; skipping binary download.');
    return;
  }

  const assetName = getAssetName();
  const binDir = join(__dirname, 'bin');
  const binPath = join(binDir, getBinaryName());

  if (!existsSync(binDir)) {
    mkdirSync(binDir, { recursive: true });
  }

  const tag = VERSION.startsWith('v') ? VERSION : `v${VERSION}`;
  const assetUrl = `https://github.com/${REPO}/releases/download/${tag}/${assetName}`;
  const checksumsUrl = `https://github.com/${REPO}/releases/download/${tag}/checksums.txt`;

  info(`Downloading ${assetName} (${tag})...`);
  const binary = await download(assetUrl);

  info('Verifying checksum...');
  const checksums = await download(checksumsUrl);
  const expectedHash = findChecksum(checksums.toString('utf8'), assetName);
  if (!expectedHash) {
    throw new Error(`no checksum found for ${assetName}`);
  }
  const actualHash = createHash('sha256').update(binary).digest('hex');
  if (actualHash.toLowerCase() !== expectedHash.toLowerCase()) {
    throw new Error(`checksum mismatch for ${assetName}: expected ${expectedHash}, got ${actualHash}`);
  }

  writeFileSync(binPath, binary);
  try {
    execSync(`chmod +x "${binPath}"`, { stdio: 'ignore' });
  } catch {
    // chmod may fail on Windows; binary is still usable via extension.
  }

  info(`Installed ${binPath}`);
}

function findChecksum(content, assetName) {
  const base = assetName.split(/[\\/]/).pop();
  for (const rawLine of content.split('\n')) {
    const line = rawLine.trim();
    if (!line) continue;
    const parts = line.split(/\s+/);
    if (parts.length < 2) continue;
    const [hash, filename] = parts;
    const cleanFilename = filename.replace(/^\*+/, '');
    if (cleanFilename.split(/[\\/]/).pop() === base) {
      return hash.trim();
    }
  }
  return null;
}

main().catch((err) => {
  error(err.message);
  process.exit(1);
});
