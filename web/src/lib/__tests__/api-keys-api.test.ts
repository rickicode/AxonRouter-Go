import { describe, it, expect, vi, beforeEach } from 'vitest';
import { apiKeysApi } from '$lib/api';
describe('apiKeysApi.create', () => {
beforeEach(() => {
vi.restoreAllMocks();
});
it('sends allowed_models in the request body', async () => {
const fetchMock = vi.fn().mockResolvedValue({
ok: true,
headers: { get: () => null },
json: () =>
Promise.resolve({
id: 'key-1',
key: 'secret',
name: 'test',
max_tokens: 1000,
message: 'ok',
}),
});
vi.stubGlobal('fetch', fetchMock);
await apiKeysApi.create('test', 60, 1000, undefined, ['gpt-4o', 'claude-sonnet']);
expect(fetchMock).toHaveBeenCalledTimes(1);
const init = fetchMock.mock.calls[0][1] as RequestInit;
const body = JSON.parse(init.body as string);
expect(body.allowed_models).toEqual(['gpt-4o', 'claude-sonnet']);
});
it('omits allowed_models from the body when not provided', async () => {
const fetchMock = vi.fn().mockResolvedValue({
ok: true,
headers: { get: () => null },
json: () =>
Promise.resolve({
id: 'key-2',
key: 'secret',
name: 'test',
max_tokens: 1000,
message: 'ok',
}),
});
vi.stubGlobal('fetch', fetchMock);
await apiKeysApi.create('test', 60, 1000);
const init = fetchMock.mock.calls[0][1] as RequestInit;
const body = JSON.parse(init.body as string);
expect(body.allowed_models).toBeUndefined();
});
it('sends budget fields in the request body', async () => {
const fetchMock = vi.fn().mockResolvedValue({
ok: true,
headers: { get: () => null },
json: () =>
Promise.resolve({
id: 'key-3',
key: 'secret',
name: 'test',
max_tokens: 1000,
message: 'ok',
daily_limit_usd: 10,
monthly_limit_usd: 100,
warning_threshold: 0.9,
}),
});
vi.stubGlobal('fetch', fetchMock);
await apiKeysApi.create('test', 60, 1000, undefined, undefined, 10, 100, 0.9);
expect(fetchMock).toHaveBeenCalledTimes(1);
const init = fetchMock.mock.calls[0][1] as RequestInit;
const body = JSON.parse(init.body as string);
expect(body.daily_limit_usd).toBe(10);
expect(body.monthly_limit_usd).toBe(100);
expect(body.warning_threshold).toBe(0.9);
});
it('omits budget fields when not provided', async () => {
const fetchMock = vi.fn().mockResolvedValue({
ok: true,
headers: { get: () => null },
json: () =>
Promise.resolve({
id: 'key-4',
key: 'secret',
name: 'test',
max_tokens: 1000,
message: 'ok',
}),
});
vi.stubGlobal('fetch', fetchMock);
await apiKeysApi.create('test', 60, 1000);
const init = fetchMock.mock.calls[0][1] as RequestInit;
const body = JSON.parse(init.body as string);
expect(body.daily_limit_usd).toBeUndefined();
expect(body.monthly_limit_usd).toBeUndefined();
expect(body.warning_threshold).toBeUndefined();
});
});
describe('apiKeysApi.toggle', () => {
beforeEach(() => {
vi.restoreAllMocks();
});
it('sends budget fields in the toggle body', async () => {
const fetchMock = vi.fn().mockResolvedValue({
ok: true,
headers: { get: () => null },
json: () => Promise.resolve({ ok: true }),
});
vi.stubGlobal('fetch', fetchMock);
await apiKeysApi.toggle('key-1', true, 5000000, 5, 50, 0.75);
expect(fetchMock).toHaveBeenCalledTimes(1);
const init = fetchMock.mock.calls[0][1] as RequestInit;
const body = JSON.parse(init.body as string);
expect(body.is_active).toBe(true);
expect(body.max_tokens).toBe(5000000);
expect(body.daily_limit_usd).toBe(5);
expect(body.monthly_limit_usd).toBe(50);
expect(body.warning_threshold).toBe(0.75);
});
it('omits optional fields when not provided', async () => {
const fetchMock = vi.fn().mockResolvedValue({
ok: true,
headers: { get: () => null },
json: () => Promise.resolve({ ok: true }),
});
vi.stubGlobal('fetch', fetchMock);
await apiKeysApi.toggle('key-1', false);
const init = fetchMock.mock.calls[0][1] as RequestInit;
const body = JSON.parse(init.body as string);
expect(body.is_active).toBe(false);
expect(body.max_tokens).toBeUndefined();
expect(body.daily_limit_usd).toBeUndefined();
expect(body.monthly_limit_usd).toBeUndefined();
expect(body.warning_threshold).toBeUndefined();
});
});
