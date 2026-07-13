import type { Connection, ProxyPool } from '$lib/api';

export function getProxyPoolId(connection: Connection): string | undefined {
  const psd = connection.provider_specific_data;
  if (!psd) return undefined;
  try {
    const parsed = JSON.parse(psd);
    return parsed?.proxyPoolId || undefined;
  } catch {
    return undefined;
  }
}

export function getMissingPools(
  pools: ProxyPool[],
  connections: Connection[]
): ProxyPool[] {
  const connectedIds = new Set(connections.map(getProxyPoolId).filter(Boolean));
  return pools.filter((pool) => !connectedIds.has(pool.id));
}

export function filterProxyPools<T extends { name: string; type: string }>(
  pools: T[],
  query: string
): T[] {
  const q = query.trim().toLowerCase();
  if (!q) return pools;
  return pools.filter(
    (pool) =>
      pool.name.toLowerCase().includes(q) ||
      pool.type.toLowerCase().includes(q)
  );
}
