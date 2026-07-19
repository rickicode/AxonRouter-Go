import { describe, it, expect } from 'vitest';
import {
  normalizeVersion,
  isUpdateAvailable,
  parseChangelogForVersion,
} from '../about-utils';

describe('about-utils', () => {
  it('strips v prefix from versions', () => {
    expect(normalizeVersion('v0.3.1')).toBe('0.3.1');
    expect(normalizeVersion('0.3.1')).toBe('0.3.1');
    expect(normalizeVersion('  v0.3.1  ')).toBe('0.3.1');
  });

  it('detects when latest is newer', () => {
    expect(isUpdateAvailable('0.3.1', '0.4.0')).toBe(true);
    expect(isUpdateAvailable('0.3.1', '0.3.2')).toBe(true);
    expect(isUpdateAvailable('0.3.1', '0.3.1')).toBe(false);
    expect(isUpdateAvailable('0.3.1', '0.3.0')).toBe(false);
    expect(isUpdateAvailable('v0.3.1', 'v0.4.0')).toBe(true);
    expect(isUpdateAvailable('0.3.1', 'v0.3.1')).toBe(false);
  });

  it('handles prerelease and build metadata', () => {
    expect(isUpdateAvailable('0.3.1', '0.4.0-beta.1')).toBe(true);
    expect(isUpdateAvailable('0.3.1', '0.4.0+build.123')).toBe(true);
    expect(isUpdateAvailable('0.4.0-beta.1', '0.4.0-beta.2')).toBe(false);
    expect(isUpdateAvailable('0.3.1', '0.4')).toBe(true);
    expect(isUpdateAvailable('0.4.0', '0.4')).toBe(false);
  });

  it('extracts structured notes from a changelog section', () => {
    const md = `# Changelog

## [Unreleased]
### Added
- foo

## [0.3.1] - 2026-07-13
### Added
- Single-source versioning.
- Build-time version embedding.

### Fixed
- Bug fix.

## [0.3.0] - 2026-07-12
### Fixed
- Bug.
`;
    const sections = parseChangelogForVersion(md, '0.3.1');
    expect(sections).toHaveLength(2);
    expect(sections[0]).toEqual({
      heading: 'Added',
      items: ['Single-source versioning.', 'Build-time version embedding.'],
    });
    expect(sections[1]).toEqual({
      heading: 'Fixed',
      items: ['Bug fix.'],
    });
  });

  it('returns empty array for missing version section', () => {
    const md = '## [0.3.1]\n### Added\n- foo\n';
    expect(parseChangelogForVersion(md, '9.9.9')).toEqual([]);
  });
});
