import { describe, it, expect } from 'vitest';
import { getProviderMeta, PROVIDER_CATALOG } from '../provider-catalog';

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

  it('has a unique qwencloud entry in the catalog', () => {
    const matches = PROVIDER_CATALOG.filter((p) => p.id === 'qwencloud');
    expect(matches).toHaveLength(1);
  });

  it('includes amazon-q with the correct metadata', () => {
    const meta = getProviderMeta('amazon-q');
    expect(meta).toBeDefined();
    expect(meta!.id).toBe('amazon-q');
    expect(meta!.displayName).toBe('Amazon Q');
    expect(meta!.prefix).toBe('aq/');
    expect(meta!.format).toBe('kiro');
    expect(meta!.authType).toBe('oauth');
    expect(meta!.category).toBe('oauth');
    expect(meta!.isBuiltIn).toBe(true);
    expect(meta!.serviceKinds).toEqual(['llm']);
    expect(meta!.icon).toBe('cloud');
    expect(meta!.color).toBe('#FF9900');
  });

  it('has a unique amazon-q entry in the catalog', () => {
    const matches = PROVIDER_CATALOG.filter((p) => p.id === 'amazon-q');
    expect(matches).toHaveLength(1);
  });
});
