export interface ChangelogSection {
  heading: string;
  items: string[];
}

export function normalizeVersion(version: string): string {
  return version.trim().replace(/^v/, '');
}

export function isUpdateAvailable(current: string, latest: string): boolean {
  const cur = normalizeVersion(current);
  const lat = normalizeVersion(latest);
  if (!cur || !lat) return false;

  const curParts = cur.split('.').map((part) => Number.parseInt(part, 10));
  const latParts = lat.split('.').map((part) => Number.parseInt(part, 10));

  const maxLength = Math.max(curParts.length, latParts.length);
  for (let i = 0; i < maxLength; i++) {
    const c = curParts[i] || 0;
    const l = latParts[i] || 0;
    if (Number.isNaN(c) || Number.isNaN(l)) return false;
    if (l > c) return true;
    if (l < c) return false;
  }

  return false;
}

function extractVersionBody(markdown: string, version: string): string | null {
  const normalized = normalizeVersion(version);
  const headerLine = `## [${normalized}]`;
  const start = markdown.indexOf(headerLine);
  if (start === -1) return null;

  const bodyStart = markdown.indexOf('\n', start);
  if (bodyStart === -1) return '';

  const nextHeader = markdown.indexOf('\n## [', bodyStart + 1);
  const end = nextHeader === -1 ? markdown.length : nextHeader;
  return markdown.slice(bodyStart + 1, end).trim();
}

export function parseChangelogForVersion(
  markdown: string,
  version: string,
): ChangelogSection[] {
  const body = extractVersionBody(markdown, version);
  if (!body) return [];

  const sections: ChangelogSection[] = [];
  const lines = body.split('\n');
  let currentHeading = '';
  let currentItems: string[] = [];

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line) continue;

    if (line.startsWith('### ')) {
      if (currentHeading) {
        sections.push({ heading: currentHeading, items: currentItems });
      }
      currentHeading = line.slice(4).trim();
      currentItems = [];
    } else if (line.startsWith('- ')) {
      currentItems.push(line.slice(2).trim());
    }
  }

  if (currentHeading) {
    sections.push({ heading: currentHeading, items: currentItems });
  }

  return sections;
}
