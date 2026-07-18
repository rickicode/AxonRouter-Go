#!/usr/bin/env node
/**
 * Bump AxonRouter-Go version across the repository.
 *
 * Single source of truth: internal/version/VERSION
 * This script syncs that value to web/package.json and updates CHANGELOG.md.
 */

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const version = process.argv[2];
if (!version) {
	console.error('Usage: node bump-version.js <version>');
	process.exit(1);
}

if (!/^\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$/.test(version)) {
	console.error(`Invalid version format: ${version}`);
	process.exit(1);
}

const root = path.resolve(__dirname, '..');

// 1. Write VERSION file (single source of truth).
const versionFile = path.join(root, 'internal/version/VERSION');
fs.writeFileSync(versionFile, `${version}\n`);
console.log(`Updated ${path.relative(root, versionFile)} -> ${version}`);

// 2. Sync web/package.json version.
const pkgPath = path.join(root, 'web/package.json');
if (fs.existsSync(pkgPath)) {
	const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
	pkg.version = version;
	fs.writeFileSync(pkgPath, `${JSON.stringify(pkg, null, 2)}\n`);
	console.log(`Updated ${path.relative(root, pkgPath)} -> ${version}`);
}

// 3. Sync npm wrapper package version.
const npmPkgPath = path.join(root, 'npm', 'axonrouter-go', 'package.json');
if (fs.existsSync(npmPkgPath)) {
  const npmPkg = JSON.parse(fs.readFileSync(npmPkgPath, 'utf8'));
  if (npmPkg.version !== version) {
    npmPkg.version = version;
    fs.writeFileSync(npmPkgPath, `${JSON.stringify(npmPkg, null, 2)}\n`);
    console.log(`Updated ${path.relative(root, npmPkgPath)} -> ${version}`);
  } else {
    console.log(`${path.relative(root, npmPkgPath)} already at ${version}`);
  }
}

// 4. Update CHANGELOG.md: move Unreleased content to the new version section.
const changelogPath = path.join(root, 'CHANGELOG.md');
let changelog = fs.existsSync(changelogPath)
	? fs.readFileSync(changelogPath, 'utf8')
	: '# Changelog\n\n## [Unreleased]\n';

if (!changelog.includes('## [Unreleased]')) {
  changelog = '# Changelog\n\n## [Unreleased]\n\n' + changelog.replace(/^# Changelog\n*/, '');
}

const unreleasedRegex = /^## \[Unreleased\]\n([\s\S]*?)(?=^## \[|$(?![\s\S]))/m;
const match = changelog.match(unreleasedRegex);
if (!match) {
	console.error('CHANGELOG.md is missing a valid ## [Unreleased] section.');
	process.exit(1);
}

const unreleasedBody = match[1];
if (!unreleasedBody.trim()) {
	console.error('CHANGELOG.md ## [Unreleased] is empty. Add release notes before bumping version.');
	process.exit(1);
}

if (changelog.includes(`## [${version}]`)) {
	console.error(`Version ${version} already exists in CHANGELOG.md.`);
	process.exit(1);
}

const date = new Date().toISOString().split('T')[0];
const replacement = `## [Unreleased]\n\n## [${version}] - ${date}\n${unreleasedBody}`;
changelog = changelog.replace(match[0], replacement);
fs.writeFileSync(changelogPath, changelog);
console.log(`Updated ${path.relative(root, changelogPath)} -> ${version}`);

// Sync the latest changelog section into README.md.
const updateReadme = spawnSync('node', [path.join(root, 'scripts/update-readme.js')], {
	stdio: 'inherit',
	shell: false,
});
if (updateReadme.status !== 0) {
	process.exit(updateReadme.status ?? 1);
}
