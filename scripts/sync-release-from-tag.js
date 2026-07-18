#!/usr/bin/env node
/**
 * Use the pushed Git tag as the source of truth for version and changelog.
 *
 * - Writes the tag version to internal/version/VERSION.
 * - Syncs web/package.json version.
 * - If CHANGELOG.md does not already have a section for this version,
 *   the existing ## [Unreleased] content is moved to a new ## [version] section.
 * - Calls update-readme.js so README.md stays in sync.
 *
 * This makes the GitHub Actions release workflow forgiving: a maintainer can
 * push a tag without first running `make release` locally.
 */

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

function getVersion() {
  if (process.argv[2]) {
    return process.argv[2].replace(/^v/, '');
  }
  const ref = process.env.GITHUB_REF_NAME || '';
  if (!ref) {
    console.error('Usage: node sync-release-from-tag.js <version>');
    console.error('Or set GITHUB_REF_NAME to a tag like v1.2.3.');
    process.exit(1);
  }
  if (!ref.startsWith('v')) {
    console.error(`Expected GITHUB_REF_NAME to start with "v", got: ${ref}`);
    process.exit(1);
  }
  return ref.slice(1);
}

const version = getVersion();

if (!/^\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$/.test(version)) {
  console.error(`Invalid version format derived from tag: ${version}`);
  process.exit(1);
}

const root = path.resolve(__dirname, '..');

// 1. Always ensure VERSION file matches the tag.
const versionFile = path.join(root, 'internal/version/VERSION');
fs.writeFileSync(versionFile, `${version}\n`);
console.log(`Updated ${path.relative(root, versionFile)} -> ${version}`);

// 2. Sync web/package.json version.
const pkgPath = path.join(root, 'web/package.json');
if (fs.existsSync(pkgPath)) {
  const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
  if (pkg.version !== version) {
    pkg.version = version;
    fs.writeFileSync(pkgPath, `${JSON.stringify(pkg, null, 2)}\n`);
    console.log(`Updated ${path.relative(root, pkgPath)} -> ${version}`);
  } else {
    console.log(`${path.relative(root, pkgPath)} already at ${version}`);
  }
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

// 4. Update CHANGELOG.md if the version section doesn't exist yet.
const changelogPath = path.join(root, 'CHANGELOG.md');
if (!fs.existsSync(changelogPath)) {
  console.error('CHANGELOG.md not found.');
  process.exit(1);
}

let changelog = fs.readFileSync(changelogPath, 'utf8');
if (!changelog.includes('## [Unreleased]')) {
  console.error('CHANGELOG.md is missing a valid ## [Unreleased] section.');
  process.exit(1);
}

  if (changelog.includes(`## [${version}]`)) {
    console.log(`CHANGELOG.md already contains ## [${version}]; skipping section move.`);
  } else {
    const unreleasedRegex = /^## \[Unreleased\]\n([\s\S]*?)(?=^## \[|$(?![\s\S]))/m;
    const match = changelog.match(unreleasedRegex);
    if (!match) {
      console.error('CHANGELOG.md is missing a valid ## [Unreleased] section.');
      process.exit(1);
    }

    const unreleasedBody = match[1];
    if (!unreleasedBody.trim()) {
      console.error('CHANGELOG.md ## [Unreleased] is empty. Add release notes before creating a release tag.');
      process.exit(1);
    }

    const date = new Date().toISOString().split('T')[0];
    const replacement = `## [Unreleased]\n\n## [${version}] - ${date}\n${unreleasedBody}`;
    changelog = changelog.replace(match[0], replacement);
    fs.writeFileSync(changelogPath, changelog);
    console.log(`Updated ${path.relative(root, changelogPath)} -> moved ## [Unreleased] to ## [${version}]`);
  }

  // 4. Keep README.md in sync with the latest version section.
  const updateReadme = spawnSync('node', [path.join(root, 'scripts/update-readme.js')], {
    stdio: 'inherit',
    shell: false,
  });
  if (updateReadme.status !== 0) {
    process.exit(updateReadme.status ?? 1);
  }
