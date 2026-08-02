# Headroom Compression Setup Guide

The AxonRouter Headroom compression service automatically detects tool-output payloads (git diffs, grep results, build logs, search results, etc.) and compresses them to reduce token usage when forwarding to upstream AI models.

## Prerequisites

- A running AxonRouter-Go server.
- The Headroom compression service enabled (default: disabled).
- An optional Headroom compression endpoint reachable on the network.

## Enable Headroom service

**Option 1: Environment-based enablement**

Set the following environment variable:

```bash
export AXON_HEADROOM_ENABLED=true
```

During the Headroom backend stage, ensure the internal Headroom service runs separately from AxonRouter and is reachable via `AXON_HEADROOM_ENDPOINT`.

**Option 2: Admin API enablement**

Enable Headroom via optimization settings (depends on admin API persistence wiring per stage 2):

```bash
curl -X POST http://localhost/api/admin/optimization/settings \
  -H "Content-Type: application/json" \
  -d '{
    "headroom_enabled": true,
    "headroom_endpoint": "http://127.0.0.1:9123",
    "headroom_timeout_ms": 30000,
    "headroom_max_payload_bytes": 524288
  }'
```

Replace the endpoint URL with the actual Headroom service address.

## Configure Headroom endpoint

Choose where the Headroom compression service runs relative to AxonRouter:

- **Local service**: Run the Headroom service on the same host; use `127.0.0.1` or `localhost`.
- **Remote service**: Deploy the Headroom service to a separate machine; use the network DNS hostname or IP address.
- **Embedded**: Integrate Headroom directly into AxonRouter (future stage, not for now).

Set the service URL via environment variable:

```bash
export AXON_HEADROOM_ENDPOINT="http://127.0.0.1:9123"
```

During the Headroom backend stage, this defaults to `127.0.0.1:9123` if the internal Headroom service addresses the configured endpoint.

## Test the /compress endpoint

The Headroom `/compress` endpoint accepts tool-output payloads and returns compressed output with metric details.

**Example request body**:

```json
{
  "data": "Hmm... monkeypatch of patchutil.patch_manager.run_applied is not tested!",
  "kind_hint": "build_log"
}
```

**Compressed output**:

```json
{
  "data": "Hmm... monkeypatch of patchutil.patch_manager.run_applied is not tested!",
  "kind": "build_log",
  "original_bytes": 60,
  "compressed_bytes": 60,
  "original_tokens": 7,
  "compressed_tokens": 7
}
```

**Test the endpoint**:

```bash
curl -X POST http://127.0.0.1:9123/compress \
  -H "Content-Type: application/json" \
  -d '{
    "data": "@@ -1,5 +1,7 @@\ngit diff output",
    "kind_hint": "git_diff"
  }' | jq
```

If AxonRouter is not yet connected to the Headroom service, this should return a 503 or connection error.

## Configure timeouts and limits

Adjust the maximum request size and timeout via environment variables:

```bash
export AXON_HEADROOM_TIMEOUT_MS=30000
export AXON_HEADROOM_MAX_PAYLOAD_BYTES=524288
```

- **Timeout**: How long the Headroom service has to process a request (default: 30000ms).
- **Max payload**: Maximum input size in bytes; larger payloads are rejected (default: 524288 bytes ~ 512KB).

During the Headroom backend stage, these values default via `AXON_HEADROOM_TIMEOUT_MS` and `AXON_HEADROOM_MAX_PAYLOAD_BYTES` in `internal/headroom/config.go`.

## Verify Headroom integration

With the Headroom service running and AxonRouter configured, the integration should work through compression-aware handlers (per stage 3 translator integration):

- Tool-output payloads introduced through AI calls are detected and compressed where appropriate.
- Compressed output preserves essential information while reducing token transfer overhead.
- Original and compressed metrics are tracked for observability.

If implemented, check the optimization dashboard for Headroom metrics (this may be in a future admin metrics endpoint stage).

## Payload kinds supported

Headroom detects and optimizes the following payload kinds:

- **`git_diff`**: Restructures unified diff output, keeping commit headers, hunk markers, and a limited context (default: 3 lines).
- **`git_log`**: Simplifies commit logs to compact format (c for commit messages, m for merges, a for authors, d for dates).
- **`git_status`**: Condenses porcelain git status output.
- **`grep`**: Collapses grep output by file, keeps file paths and line:column:text references.
- **`find_tree`**: Strips redundant find metadata, keeps file paths.
- **`build_log`**: Drops progress noise (percentage brackets) and collapsed duplicate consecutive lines with a count.
- **`search_results`**: Optionally filters/drops/removes URLs, truncates long lines.

The detector runs automatically if `kind_hint` is empty; all reqs return empty if the detector can't classify (`unknown` kind returns a minimal trimBlankRuns pass).

## Monitor compression savings

Backend metrics in stage 4 (admin metrics endpoint) expose Headroom compression stats across requests. Until then, typical savings come from:

- Git diffs: Large diffs with extensive context may shrink to a minimal hunk representation.
- Build logs: Repeated noise and progress bars are compressed to a single (xN) indicator.
- Search results: URLs are frequently dropped, reducing byte count that translates to tokens.
Test with varied payloads to confirm expected token and byte savings in your environment.

## Verification checklist

- [ ] Headroom service is running on the configured endpoint.
- [ ] `AXON_HEADROOM_ENABLED=true` is set (or status persisted via admin settings).
- [ ] `/compress` endpoint responds to valid requests.
- [ ] AxonRouter handlers can reach the Headroom service without timeout or connection failures.
- [ ] Git diffs, build logs, search results, and similar payloads are being compressed where possible.
- [ ] Compression metrics are tracked (if admin metrics endpoint is wired).

