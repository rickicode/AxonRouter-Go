# Headroom External Compression

AxonRouter-Go includes a Headroom-style external compression proxy that reduces
token input from common tool outputs before forwarding requests to upstream LLM
providers.

## What it compresses

- `git diff`, `git log`, `git status`
- `grep` output
- `find` / `tree` listings
- Build / test logs
- Search results
- Anthropic `tool_result` content blocks

## Configuration

Via environment variables (evaluated at startup, overridden by dashboard settings):

| Variable | Default | Description |
|----------|---------|-------------|
| `AXON_HEADROOM_ENABLED` | `false` | Enable the service |
| `AXON_HEADROOM_ENDPOINT` | *(internal)* | HTTP endpoint of a Headroom-compatible service |
| `AXON_HEADROOM_TIMEOUT_MS` | `30000` | Per-request timeout |
| `AXON_HEADROOM_MAX_PAYLOAD_BYTES` | `524288` | Max bytes sent for compression |

When no endpoint is configured, AxonRouter starts an in-process HTTP server on
`127.0.0.1:9123` (or the next available port if 9123 is busy).

## Dashboard

Go to **Settings → Compression**. Enable the **Headroom external compression**
toggle to activate the service. The card shows:

- running / stopped / error status
- effective endpoint
- total calls, bytes saved, and errors

## Fail-open semantics

Headroom is fail-open. If the service is unreachable, times out, or produces a
larger payload, the original text is forwarded unchanged. Compression metrics
still record the attempt so operators can see errors.

## Metrics

- `headroom_total` — total compression calls
- `headroom_bytes_saved` — bytes removed from payloads
- `headroom_errors` — failed compression attempts

These are exposed through the existing `/compression/metrics` admin endpoint.

## API

The internal server exposes:

- `POST /compress` — accepts a `PayloadHeader` JSON body and returns a
  `CompressedResult` with the compressed text and savings metadata.
- `GET /health` — simple health probe.
