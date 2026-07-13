#!/usr/bin/env node
/**
 * Extract the changelog section for a specific version.
 *
 * Usage: node extract-changelog.js [version]
 *   version defaults to the value in internal/version/VERSION.
 */

const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');

let version = process.argv[2];
if (!version) {
	const versionFile = path.join(root, 'internal/version/VERSION');
	version = fs.readFileSync(versionFile, 'utf8').trim();
}

const changelogPath = path.join(root, 'CHANGELOG.md');
if (!fs.existsSync(changelogPath)) {
	console.error('CHANGELOG.md not found.');
	process.exit(1);
}

const changelog = fs.readFileSync(changelogPath, 'utf8');
const sectionRegex = new RegExp(`^## \\[${version.replace(/[.*+?^${}()|[\]\\\\]/g, '\\$&')}\\][\\s\\S]*?(?=^## \\[|$(?![\\s\\S]))`, 'm');
const match = changelog.match(sectionRegex);

if (!match) {
	console.error(`No changelog section found for version ${version}.`);
	process.exit(1);
}

const body = match[0]
	.split('\n')
	.slice(1) // Drop the "## [x.y.z] - date" header line.
	.join('\n')
	.replace(/\n+$/, '');

console.log(body.trim());
