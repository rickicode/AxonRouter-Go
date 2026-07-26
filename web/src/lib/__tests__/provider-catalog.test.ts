import { describe, it, expect } from 'vitest';
import { getProviderMeta, PROVIDER_CATALOG, loadProviderAliases } from '../provider-catalog';

describe('provider-catalog', () => {
  it('includes qwencloud with the correct metadata', () => {
    const meta = getProviderMeta('qwencloud');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('qwencloud');
    expect(meta!.displayName).toBe('QwenCloud');
    expect(meta!.prefix).toBe('qwencloud/');
    expect(meta!.format).toBe('openai-responses');
    expect(meta!.authType).toBe('apikey');
    expect(meta!.category).toBe('apikey');
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
    expect(meta!.iconFile).toBe(
      'https://img.alicdn.com/imgextra/i2/O1CN01F3ylft1COZGWn6kop_!!6000000000071-2-tps-48-48.png',
    );
  });

  it('includes qoder with dual-mode OAuth and API key metadata', () => {
    const meta = getProviderMeta('qoder');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('qoder');
    expect(meta!.displayName).toBe('Qoder');
    expect(meta!.prefix).toBe('qoder/');
    expect(meta!.format).toBe('qoder');
    expect(meta!.authType).toBe('oauth');
    expect(meta!.category).toBe('oauth');
    expect(meta!.authModes).toEqual(['oauth', 'apikey']);
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
  });

  it('has a unique qoder entry in the catalog', () => {
    const matches = PROVIDER_CATALOG.filter((p) => p.id === 'qoder');
    expect(matches).toHaveLength(1);
  });

  it('has a unique qwencloud entry in the catalog', () => {
    const matches = PROVIDER_CATALOG.filter((p) => p.id === 'qwencloud');
    expect(matches).toHaveLength(1);
  });
  it('includes commandcode with the correct metadata', () => {
    const meta = getProviderMeta('commandcode');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('commandcode');
    expect(meta!.displayName).toBe('CommandCode AI');
    expect(meta!.prefix).toBe('commandcode/');
    expect(meta!.format).toBe('openai');
    expect(meta!.authType).toBe('apikey');
    expect(meta!.category).toBe('apikey');
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
    expect(meta!.aliases).toEqual(['cmd']);
    expect(meta!.iconFile).toBe('/providers/commandcode.svg');
  });
  it('resolves cmd alias to commandcode', () => {
    loadProviderAliases([{ id: 'commandcode', aliases: ['cmd'] }]);
    expect(getProviderMeta('cmd')?.id).toBe('commandcode');
  });
  it('has a unique commandcode entry in the catalog', () => {
    const matches = PROVIDER_CATALOG.filter((p) => p.id === 'commandcode');
    expect(matches).toHaveLength(1);
  });
});
