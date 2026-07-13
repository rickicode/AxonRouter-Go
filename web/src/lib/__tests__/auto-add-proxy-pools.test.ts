import { describe, it, expect } from 'vitest';
import { getProxyPoolId, getMissingPools, filterProxyPools } from '$lib/auto-add-proxy-pools';
import type { Connection, ProxyPool } from '$lib/api';

function makeConnection(psd: string | null): Connection {
  return {
    id: 'c1',
    provider_type_id: 'oc',
    name: 'Test',
    auth_type: 'none',
    status: 'ready',
    cooldown_until: null,
    last_error: null,
    last_error_code: null,
    last_success_at: null,
    last_failure_at: null,
    failure_count: 0,
    capabilities: '',
    provider_specific_data: psd,
    oauth_expires_at: null,
    priority: 1,
    is_active: true,
    created_at: 1,
    updated_at: 1,
  };
}

function makePool(id: string): ProxyPool {
  return {
    id,
    name: id,
    type: 'http',
    proxyUrl: 'http://x',
    noProxy: '',
    relayAuth: '',
    isActive: true,
    testStatus: 'active',
    lastTestedAt: null,
    lastError: null,
    responseTimeMs: null,
    createdAt: 1,
  };
}

describe('getProxyPoolId', () => {
  it('returns undefined when provider_specific_data is null', () => {
    expect(getProxyPoolId(makeConnection(null))).toBeUndefined();
  });

  it('returns undefined when JSON has no proxyPoolId', () => {
    expect(getProxyPoolId(makeConnection('{"foo":"bar"}'))).toBeUndefined();
  });

  it('returns proxyPoolId when present', () => {
    expect(getProxyPoolId(makeConnection('{"proxyPoolId":"pool-1"}'))).toBe('pool-1');
  });
});

describe('getMissingPools', () => {
  it('returns all pools when no connections reference pools', () => {
    const pools = [makePool('a'), makePool('b')];
    expect(getMissingPools(pools, [])).toEqual(pools);
  });

  it('excludes pools already connected', () => {
    const pools = [makePool('a'), makePool('b')];
    const conns = [makeConnection('{"proxyPoolId":"a"}')];
    expect(getMissingPools(pools, conns)).toEqual([makePool('b')]);
  });

  it('ignores malformed provider_specific_data', () => {
    const pools = [makePool('a')];
    const conns = [makeConnection('not-json')];
    expect(getMissingPools(pools, conns)).toEqual([makePool('a')]);
  });
});

describe('filterProxyPools', () => {
  it('returns all pools when query is empty', () => {
    const pools = [makePool('a'), makePool('b')];
    expect(filterProxyPools(pools, '')).toEqual(pools);
  });

  it('filters by name', () => {
    const pools = [makePool('alpha'), makePool('beta')];
    expect(filterProxyPools(pools, 'alp')).toEqual([makePool('alpha')]);
  });

  it('filters by type', () => {
    const pools = [{ ...makePool('a'), type: 'socks5' }, makePool('b')];
    expect(filterProxyPools(pools, 'socks')).toEqual([pools[0]]);
  });

  it('matches regardless of case', () => {
    const pools = [makePool('Alpha')];
    expect(filterProxyPools(pools, 'AL')).toEqual(pools);
  });

  it('returns empty array when no pools match', () => {
    const pools = [makePool('a')];
    expect(filterProxyPools(pools, 'zzz')).toEqual([]);
  });
});
