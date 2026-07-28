
## Codex Live Session Store

The `CODEX_LIVE_STORE_PROVIDER` environment variable selects where Codex Live session state is kept:

| Variable | Default | Description |
|----------|---------|-------------|
| `CODEX_LIVE_STORE_PROVIDER` | `memory` | `memory`, `sqlite`, or `redis` |
| `CODEX_LIVE_STORE_TTL_MS` | `3600000` (1 hour) | Session TTL in milliseconds |
| `CODEX_LIVE_REDIS_ADDR` | `localhost:6379` | Redis server address |
| `CODEX_LIVE_REDIS_PASSWORD` | `""` | Redis password |
| `CODEX_LIVE_REDIS_DB` | `0` | Redis database number |

- `memory` (default): fast, but sessions are lost when the process restarts.
- `sqlite`: stores sessions in the main AxonRouter SQLite database, so live calls can reconnect across process restarts.
- `redis`: store sessions in Redis; useful for multi-instance deployments.

Sessions are automatically TTL-expired in all backends.
