import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';

const source = readFileSync('./src/pages/ComboDetail.svelte', 'utf-8');

describe('ComboDetail redirect', () => {
  it('redirects /combos/:id to /combos and removes the inline edit form', () => {
    expect(source).toContain("router.navigate('/combos')");
    expect(source).not.toContain('<Card');
    expect(source).not.toContain('editing');
  });
});
