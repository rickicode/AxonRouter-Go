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

  it('includes the Phase-1 search providers', () => {
    const searchIds = ['tavily', 'brave', 'exa', 'serper', 'google-pse', 'searxng'];
    for (const id of searchIds) {
      const meta = getProviderMeta(id);
      expect(meta).toBeDefined();
      expect(meta!.category).toBe('search');
      expect(meta!.serviceKinds).toEqual(['webSearch']);
      expect(meta!.isBuiltIn).toBe(true);
      expect(meta!.prefix).toBe(`${id}/`);
      expect(meta!.format).toBe('search');
    }
  });

  it('marks tavily, brave, google-pse, and searxng as free-tier capable', () => {
    for (const id of ['tavily', 'brave', 'google-pse', 'searxng']) {
      const meta = getProviderMeta(id);
      expect(meta?.hasFree).toBe(true);
    }
  });

  it('uses custom auth for Google PSE and no auth for SearXNG', () => {
    expect(getProviderMeta('google-pse')?.authType).toBe('custom');
    expect(getProviderMeta('searxng')?.authType).toBe('none');
  });

  it('has unique entries for each Phase-1 search provider', () => {
    for (const id of ['tavily', 'brave', 'exa', 'serper', 'google-pse', 'searxng']) {
      const matches = PROVIDER_CATALOG.filter((p) => p.id === id);
      expect(matches).toHaveLength(1);
    }
  });

  it('includes gitlab with OAuth metadata', () => {
    const meta = getProviderMeta('gitlab');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('gitlab');
    expect(meta!.displayName).toBe('GitLab Duo');
    expect(meta!.prefix).toBe('gitlab/');
    expect(meta!.format).toBe('openai');
    expect(meta!.authType).toBe('oauth');
    expect(meta!.category).toBe('oauth');
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
  });

  it('includes xai with OAuth metadata', () => {
    const meta = getProviderMeta('xai');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('xai');
    expect(meta!.displayName).toBe('xAI (Grok)');
    expect(meta!.prefix).toBe('xai/');
    expect(meta!.format).toBe('openai');
    expect(meta!.authType).toBe('oauth');
    expect(meta!.category).toBe('oauth');
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
  });

  it('includes iflow with OAuth metadata', () => {
    const meta = getProviderMeta('iflow');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('iflow');
    expect(meta!.displayName).toBe('iFlow AI');
    expect(meta!.prefix).toBe('iflow/');
    expect(meta!.format).toBe('openai');
    expect(meta!.authType).toBe('oauth');
    expect(meta!.category).toBe('oauth');
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
  });

  it('has unique gitlab/xai/iflow entries in the catalog', () => {
    for (const id of ['gitlab', 'xai', 'iflow']) {
      const matches = PROVIDER_CATALOG.filter((p) => p.id === id);
      expect(matches).toHaveLength(1);
    }
  });
});
