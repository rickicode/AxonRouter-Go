#!/usr/bin/env node
/**
 * Syncs npm/axonrouter-go/package.json version from internal/version/VERSION.
 *
 * This keeps the npm wrapper package's version aligned with the Go binary release.
 * It is used by `make set-version` and by the release workflow before publishing.
 */

const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const versionFile = path.join(root, 'internal', 'version', 'VERSION');
const packageJsonFile = path.join(root, 'npm', 'axonrouter-go', 'package.json');

function main() {
  const version = fs.readFileSync(versionFile, 'utf8').trim();
  if (!/^\d+\.\d+\.\d+/.test(version)) {
    console.error(`error: invalid version in ${versionFile}: ${version}`);
    process.exit(1);
  }

  const pkg = JSON.parse(fs.readFileSync(packageJsonFile, 'utf8'));
  if (pkg.version === version) {
    console.log(`npm package version already synced: ${version}`);
    return;
  }

  pkg.version = version;
  fs.writeFileSync(packageJsonFile, `${JSON.stringify(pkg, null, 2)}\n`);
  console.log(`Updated ${path.relative(root, packageJsonFile)} -> ${version}`);
}

main();
