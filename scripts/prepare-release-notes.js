#!/usr/bin/env node
/**
 * Prepare release notes for the current VERSION.
 *
 * 1. Try to extract the matching section from CHANGELOG.md.
 * 2. If the section does not exist, fall back to the latest git commit message.
 * 3. Write the result to RELEASE_NOTES.md and emit a GitHub Actions notice
 *    so the release page clearly shows which source was used.
 *
 * Usage: node prepare-release-notes.js
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const root = path.resolve(__dirname, '..');
const versionFile = path.join(root, 'internal/version/VERSION');
const changelogPath = path.join(root, 'CHANGELOG.md');
const outputPath = path.join(root, 'RELEASE_NOTES.md');

if (!fs.existsSync(versionFile)) {
	console.error('internal/version/VERSION not found.');
	process.exit(1);
}

const version = fs.readFileSync(versionFile, 'utf8').trim();

let notes = '';
let source = '';

// 1. Prefer CHANGELOG.md section for the current version.
if (fs.existsSync(changelogPath)) {
	const changelog = fs.readFileSync(changelogPath, 'utf8');
	const sectionRegex = new RegExp(
		`^## \\[${version.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\][\\s\\S]*?(?=^## \\[|$(?![\\s\\S]))`,
		'm'
	);
	const match = changelog.match(sectionRegex);
	if (match) {
		notes = match[0]
			.split('\n')
			.slice(1) // drop ## [x.y.z] - date header
			.join('\n')
			.replace(/\n+$/, '')
			.trim();
		source = 'CHANGELOG.md';
	}
}

// 2. Fall back to the latest commit message.
if (!notes) {
	try {
		notes = execSync('git log -1 --pretty=%B', { cwd: root, encoding: 'utf8' }).trim();
		source = 'latest git commit';
	} catch (err) {
		console.error('Failed to read git log:', err.message);
		process.exit(1);
	}
}

// Add a clear header so the release page shows the version source.
const header = `## What's Changed in v${version}\n> Release notes source: ${source}\n`;
fs.writeFileSync(outputPath, `${header}\n${notes}\n`);

if (process.env.GITHUB_ACTIONS) {
	console.log(`::notice::Release notes for v${version} generated from ${source}`);
}

console.log(`Release notes for v${version} prepared from ${source}.`);
