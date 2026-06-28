# AxonRouter-Go Deployment Guide

## Prerequisites

- Go 1.22+
- Node.js 18+ (untuk frontend build)
- SQLite (embedded, no setup needed)

## Build

### Full Build

```bash
# Clone
git clone https://github.com/rickicode/AxonRouter-Go.git
cd AxonRouter-Go

# Install frontend dependencies
cd web && npm install && cd ..

# Build everything
make build
```

Output: `build/axonrouter` (single binary)

### Frontend Only

```bash
make frontend
```

### Backend Only

```bash
make backend
```

## Running

### Direct Run

```bash
./build/axonrouter
```

Server starts di port **3777**. Dashboard: http://localhost:3777

### With Custom Port

```bash
PORT=8080 ./build/axonrouter
```

### Background Run

```bash
nohup ./build/axonrouter > axonrouter.log 2>&1 &
```

### Systemd Service

```ini
# /etc/systemd/system/axonrouter.service
[Unit]
Description=AxonRouter-Go API Proxy
After=network.target

[Service]
Type=simple
User=axonrouter
WorkingDirectory=/opt/axonrouter
ExecStart=/opt/axonrouter/build/axonrouter
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable axonrouter
sudo systemctl start axonrouter
sudo systemctl status axonrouter
```

### Docker

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN apk add --no-cache nodejs npm
RUN cd web && npm install && npm run build && cd ..
RUN go build -o build/axonrouter ./cmd/server

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/build/axonrouter .
EXPOSE 3777
CMD ["./axonrouter"]
```

```bash
docker build -t axonrouter .
docker run -p 3777:3777 -v axonrouter-data:/app axonrouter
```

### Docker Compose

```yaml
version: '3.8'
services:
  axonrouter:
    build: .
    ports:
      - "3777:3777"
    volumes:
      - axonrouter-data:/app
    restart: unless-stopped

volumes:
  axonrouter-data:
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3777` | Server port |
| `ADMIN_KEY` | (empty) | Admin API key |
| `DB_PATH` | `axonrouter.db` | SQLite database path |

### Settings (SQLite)

Settings stored di `settings` table, manageable via admin API:

```bash
# List settings
curl http://localhost:3777/api/admin/settings

# Update setting
curl -X PUT http://localhost:3777/api/admin/settings/quota_check_interval \
  -H "Content-Type: application/json" \
  -d '{"value": "15m"}'
```

| Key | Default | Description |
|-----|---------|-------------|
| `quota_check_interval` | `30m` | Background quota check interval |
| `usage_flush_interval` | `5s` | Usage log flush interval |
| `circuit_breaker_cleanup_interval` | `5m` | Circuit breaker cleanup |

## Initial Setup

### 1. Start Server

```bash
./build/axonrouter
```

### 2. Add API Key

```bash
# Via SQLite
sqlite3 axonrouter.db "INSERT INTO api_keys (id, key_hash, is_active, created_at, updated_at) VALUES ('key-1', 'your-api-key', 1, strftime('%s','now'), strftime('%s','now'));"
```

Or via admin dashboard at http://localhost:3777/settings

### 3. Add Provider

```bash
curl -X POST http://localhost:3777/api/admin/providers \
  -H "Content-Type: application/json" \
  -d '{
    "name": "openai",
    "format": "openai",
    "base_url": "https://api.openai.com/v1"
  }'
```

### 4. Add Connection

```bash
curl -X POST http://localhost:3777/api/admin/providers/openai/connections \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-key-001",
    "api_key": "sk-xxx",
    "auth_type": "api_key"
  }'
```

### 5. Test Connection

```bash
curl -X POST http://localhost:3777/api/admin/connections/conn-id/test
```

### 6. Start Using

```bash
curl -X POST http://localhost:3777/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Backup

### Database Backup

```bash
# SQLite backup
sqlite3 axonrouter.db ".backup backup.db"

# Or just copy the file
cp axonrouter.db axonrouter.db.backup
```

### Automated Backup

```bash
# Cron job (daily at 2am)
0 2 * * * sqlite3 /opt/axonrouter/axonrouter.db ".backup /backup/axonrouter-$(date +\%Y\%m\%d).db"
```

## Monitoring

### Health Check

```bash
curl http://localhost:3777/v1/models
```

### Logs

```bash
# View logs
curl "http://localhost:3777/api/admin/logs?per_page=100"

# Stats
curl http://localhost:3777/api/admin/logs/stats
```

### Dashboard

Access dashboard at http://localhost:3777

- **Home** — Overview, connection counts per status
- **Providers** — Provider list, connection management
- **Combos** — Combo routing configuration
- **Logs** — Request history with filters
- **Settings** — API keys, rate limits

## Troubleshooting

### Server won't start

```bash
# Check if port is in use
lsof -i :3777

# Check logs
tail -f axonrouter.log
```

### Database locked

```bash
# Check for WAL file
ls -la axonrouter.db*

# Recovery
sqlite3 axonrouter.db "PRAGMA wal_checkpoint(TRUNCATE);"
```

### High memory usage

```bash
# Check connection count
curl http://localhost:3777/api/admin/dashboard/stats

# Reduce connections or increase cleanup frequency
curl -X PUT http://localhost:3777/api/admin/settings/circuit_breaker_cleanup_interval \
  -d '{"value": "2m"}'
```

### Rate limit issues

```bash
# Check rate limit settings
sqlite3 axonrouter.db "SELECT * FROM api_keys;"

# Update rate limit
sqlite3 axonrouter.db "UPDATE api_keys SET rate_limit_per_min = 1000 WHERE id = 'key-1';"
```

## Upgrading

```bash
# Stop server
sudo systemctl stop axonrouter

# Backup database
cp axonrouter.db axonrouter.db.backup

# Pull latest code
git pull

# Rebuild
make build

# Start server
sudo systemctl start axonrouter
```

Database migrations run automatically on startup.

## Performance Tuning

### For High Traffic (1000+ connections)

1. Increase file descriptor limit:
```bash
ulimit -n 65536
```

2. Tune SQLite:
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = -64000;  -- 64MB
PRAGMA busy_timeout = 5000;
```

3. Monitor memory:
```bash
# Check memory usage
ps aux | grep axonrouter

# Check connection count
curl http://localhost:3777/api/admin/dashboard/stats
```

### For Low Latency

1. Use local SQLite (not network filesystem)
2. Enable connection pooling (built-in)
3. Reduce logging verbosity
4. Use SSD for database storage
