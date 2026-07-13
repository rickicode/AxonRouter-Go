#!/usr/bin/env node
/**
 * Sync the latest changelog section into README.md between marker comments.
 *
 * Usage:
 *   node update-readme.js           # update README.md in place
 *   node update-readme.js --check   # exit 1 if README is out of sync
 */

const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const checkMode = process.argv.includes('--check');

const readmePath = path.join(root, 'README.md');
const changelogPath = path.join(root, 'CHANGELOG.md');
const versionPath = path.join(root, 'internal/version/VERSION');

if (!fs.existsSync(readmePath)) {
	console.error('README.md not found.');
	process.exit(1);
}
if (!fs.existsSync(changelogPath)) {
	console.error('CHANGELOG.md not found.');
	process.exit(1);
}
if (!fs.existsSync(versionPath)) {
	console.error('internal/version/VERSION not found.');
	process.exit(1);
}

const version = fs.readFileSync(versionPath, 'utf8').trim();
const changelog = fs.readFileSync(changelogPath, 'utf8');

const sectionRegex = new RegExp(
	`^## \\[${version.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\][\\s\\S]*?(?=^## \\[|$(?![\\s\\S]))`,
	'm'
);
const match = changelog.match(sectionRegex);
if (!match) {
	console.error(`No changelog section found for version ${version}.`);
	process.exit(1);
}

const sectionBody = match[0]
	.split('\n')
	.slice(1) // drop ## [x.y.z] - date line
	.join('\n')
	.trim();

const replacement = `<!-- LATEST_CHANGELOG_START -->\n### What's New in v${version}\n\n${sectionBody}\n<!-- LATEST_CHANGELOG_END -->`;

const readme = fs.readFileSync(readmePath, 'utf8');
const markerRegex = /<!-- LATEST_CHANGELOG_START -->[\s\S]*?<!-- LATEST_CHANGELOG_END -->/;

if (!markerRegex.test(readme)) {
	console.error(
		'README.md is missing LATEST_CHANGELOG markers. ' +
		'Add <!-- LATEST_CHANGELOG_START --> and <!-- LATEST_CHANGELOG_END --> around the changelog area.'
	);
	process.exit(1);
}

const updatedReadme = readme.replace(markerRegex, replacement);

if (readme === updatedReadme) {
	console.log('README.md is already up to date.');
	process.exit(0);
}

if (checkMode) {
	console.error('README.md is out of sync with CHANGELOG.md. Run `node scripts/update-readme.js` to fix.');
	process.exit(1);
}

fs.writeFileSync(readmePath, updatedReadme);
console.log(`README.md updated with changelog for v${version}.`);
