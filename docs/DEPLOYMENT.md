# AxonRouter-Go Deployment Guide

For the project overview and quick start, see [README.md](../README.md).
For tool-by-tool client settings, see [docs/INTEGRATIONS.md](./INTEGRATIONS.md).

## Recommended install

```bash
curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh | bash
```

The installer auto-detects your OS/architecture, picks the matching GitHub release asset, and installs `axonrouter` into the first writable directory on this list:

1. `~/.local/bin`
2. `/usr/local/bin`

### Common options

| Command | What it does |
|---|---|
| `./installer.sh` | Latest release, auto-detected OS/arch. |
| `./installer.sh --version v0.3.3` | Pin a specific release tag. |
| `./installer.sh --to /usr/local/bin` | Install to a custom directory. |
| `curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh \| sudo bash -s -- --service` | Install binary + create a systemd service (Linux only). |

### Supported targets

Release binaries are built for:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`

> **Requirements:** `curl` must be installed. On Windows, run the installer from Git Bash or WSL.

## Prerequisites

- Go 1.23+
- Node.js 18+ (for frontend build)
- SQLite (embedded, no setup needed)
- curl (for the one-line installer)

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

Server starts on port **3777**. Dashboard: http://localhost:3777

### With Custom Port

```bash
AXON_PORT=8080 ./build/axonrouter
```

### Background Run

```bash
nohup ./build/axonrouter > axonrouter.log 2>&1 &
```

## Binary CLI

The release binary has a small built-in CLI for help, systemd management, and password changes.

```bash
# Show all options
axonrouter --help

# Install and manage the systemd service (Linux only)
axonrouter --startup install
axonrouter --startup status
axonrouter --startup start
axonrouter --startup stop
axonrouter --startup restart

# Change the admin dashboard password
axonrouter --setpass <password>
```

`--startup install` writes `/etc/systemd/system/axonrouter.service`, runs as the invoking user (or `SUDO_USER` when called via `sudo`), and uses the binary default data directory (`~/axonrouter`).

## Systemd Service

### Recommended: binary-managed service

```bash
sudo axonrouter --startup install
systemctl status axonrouter
```

### Manual service file (fallback)

```ini
# /etc/systemd/system/axonrouter.service
[Unit]
Description=AxonRouter-Go API Proxy
After=network.target

[Service]
Type=simple
User=axonrouter
WorkingDirectory=/home/axonrouter
ExecStart=/usr/local/bin/axonrouter
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable axonrouter
sudo systemctl start axonrouter
sudo systemctl status axonrouter
```

## Docker

The repository includes a multi-stage `Dockerfile`.

```dockerfile
# Dockerfile
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

FROM golang:1.23-alpine AS backend-builder
RUN apk add --no-cache ca-certificates git make
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /app/web/build ./web/build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o build/axonrouter ./cmd/server

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend-builder /app/build/axonrouter .
EXPOSE 3777
VOLUME ["/app/data"]
ENTRYPOINT ["./axonrouter"]
```

```bash
docker build -t axonrouter .
docker run -d -p 3777:3777 -e HOME=/app/data -v axonrouter-data:/app/data --name axonrouter axonrouter
```

The binary stores data in `$HOME/axonrouter`, so the volume above mounts `/app/data` and points `HOME` there.

## Docker Compose

```yaml
services:
  axonrouter:
    build: .
    ports:
      - "3777:3777"
    environment:
      - HOME=/app/data
    volumes:
      - axonrouter-data:/app/data
    restart: unless-stopped

volumes:
  axonrouter-data:
```

```bash
docker compose up -d
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AXON_PORT` | `3777` | HTTP server port (read by the binary). |
| `PORT` | `3777` | Heroku/PaaS-style port name. Use `AXON_PORT` to override the binary port. |
| `AXON_ADMIN_KEY` | (empty) | Documented admin key name in `ARCHITECTURE.md`. Admin auth currently uses the session JWT; set the password with `axonrouter --setpass`. |
| `ADMIN_KEY` | (empty) | Legacy alias for the admin key. |
| `AXON_PUBLIC_IP` | (auto-detected) | Override public IP detection used by the HTTPS/ACME setup. |
| `AXONROUTER_DIR` | `~/axonrouter` | Override the data directory used for the database, logs, and PID file. Takes precedence over `HOME`. Relative paths are resolved against `$HOME`. |
| `DB_PATH` | `~/axonrouter/axonrouter.db` | Database file location. The path is derived from the data directory; this variable itself is informational. |
| `HOME` | (system) | Determines the default data directory: `$HOME/axonrouter` when `AXONROUTER_DIR` is unset. |

Deprecated variables:

- `AXON_DATA_DIR` — no longer read. Use `AXONROUTER_DIR` to override the data directory or `HOME` to relocate it.

### Data directory

By default the binary creates and uses `~/axonrouter`:

```
~/axonrouter/
├── axonrouter.db
├── axonrouter.db-shm
├── axonrouter.db-wal
├── axonrouter.pid
├── https.yml
└── logs/
```

To place data elsewhere, set `HOME` to the parent directory (the binary always appends `axonrouter`).

### Runtime Settings (SQLite)

Settings are stored in the `settings` table and manageable via the admin dashboard or the admin API:

```bash
# List settings
curl http://localhost:3777/api/admin/settings

# Update a setting
curl -X PUT http://localhost:3777/api/admin/settings/quota_check_interval \
  -H "Content-Type: application/json" \
  -d '{"value": "15m"}'
```

| Key | Default | Description |
|---|---|---|
| `quota_check_interval` | `30m` | Background quota check interval |
| `usage_flush_interval` | `5s` | Usage log flush interval |
| `circuit_breaker_cleanup_interval` | `5m` | Circuit breaker cleanup |

## Initial Setup

### 1. Start Server

```bash
./build/axonrouter
```

### 2. Create an API Key

Open the dashboard at http://localhost:3777 and create an API key in **Dashboard → Settings** (API Keys card).

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
sqlite3 ~/axonrouter/axonrouter.db ".backup backup.db"

# Or just copy the file
cp ~/axonrouter/axonrouter.db ~/axonrouter/axonrouter.db.backup
```

### Automated Backup

```bash
# Cron job (daily at 2am)
0 2 * * * sqlite3 /home/axonrouter/axonrouter/axonrouter.db ".backup /backup/axonrouter-$(date +\%Y\%m\%d).db"
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
- **Settings** — API keys, rate limits, password, HTTPS

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
ls -la ~/axonrouter/axonrouter.db*

# Recovery
sqlite3 ~/axonrouter/axonrouter.db "PRAGMA wal_checkpoint(TRUNCATE);"
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
# Check API key rate limits via the dashboard or admin API
curl http://localhost:3777/api/admin/api-keys

# Update rate limit
curl -X PATCH http://localhost:3777/api/admin/settings/rate_limit_per_min \
  -H "Content-Type: application/json" \
  -d '{"value": "1000"}'
```

## Upgrading

```bash
# Stop server
sudo systemctl stop axonrouter

# Backup database
cp ~/axonrouter/axonrouter.db ~/axonrouter/axonrouter.db.backup

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
PRAGMA cache_size = -64000; -- 64MB
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
