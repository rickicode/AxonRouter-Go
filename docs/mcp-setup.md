# MCP stdio-SSE Bridge Setup Guide

The AxonRouter-Go MCP bridge lets you register local MCP stdio servers (e.g. filesystem, browser, GitHub) and expose them to remote AI clients through a secured SSE endpoint.

## Prerequisites

- A running AxonRouter-Go server.
- An installed MCP stdio server package. Examples use `npx`, but any local executable will work.
- A valid dashboard session or master API key.

## Register a server

1. Open the dashboard and navigate to **MCP** in the sidebar.
2. Click **Add Server**.
3. Fill in the form:
   - **Name** — a unique label, e.g. `filesystem`.
   - **Command** — the executable, e.g. `npx`.
   - **Arguments** — JSON array, e.g. `["-y", "@modelcontextprotocol/server-filesystem", "/home/user/docs"]`.
   - **Environment variables** — optional JSON object with extra env vars.
   - **Restart policy** — `on-failure` (default), `always`, or `never`.
   - **Max concurrent clients** — how many simultaneous SSE sessions are allowed (default `4`).
   - **Max idle seconds** — how long a session stays open after the last message (default `60`).
4. Enable the server and click **Create Server**.
5. Click **Test** to verify the command starts without errors.

## Example: filesystem server

| Field | Value |
|-------|-------|
| Name | `filesystem` |
| Command | `npx` |
| Arguments | `["-y", "@modelcontextprotocol/server-filesystem", "/home/user/axonrouter"]` |

Click **Copy SSE URL** to get an authenticated URL you can paste into Claude Code, Codex, Cline, or any MCP-compatible client.

## Example: browser server

| Field | Value |
|-------|-------|
| Name | `browser` |
| Command | `npx` |
| Arguments | `["-y", "@modelcontextprotocol/server-puppeteer"]` |

## Connect a client

The SSE endpoint follows the Anthropic MCP protocol:

1. Client opens `GET /api/admin/mcp/:id/sse` (with `Authorization: Bearer <token>` or `?token=<token>` for EventSource).
2. Server sends an `endpoint` event containing the message POST URL, e.g. `/api/admin/mcp/:id/message?sessionId=xxx`.
3. Client sends JSON-RPC messages via `POST` to that URL.
4. Server forwards responses back through the SSE stream as `message` events.

For Claude Code / Codex / Cline, paste the copied SSE URL into the tool's MCP server configuration and supply an AxonRouter session token.

## Lifecycle behavior

- A new subprocess is started for each SSE session.
- If the configured `max_clients` is reached, new SSE connections are rejected.
- Subprocess stdout is streamed to the client; stderr is logged server-side only.
- When the client disconnects, or after `max_idle_sec` of inactivity, the subprocess is terminated.
- `restart_policy=always` re-spawns immediately on crash; `on-failure` preserves crash info and allows future reconnects; `never` leaves the session closed.

## Security

- The SSE endpoint requires a valid admin session JWT or master API key. Keep the copied URL/token private.
- Command and arguments are validated to reject paths that contain shell metacharacters (`;`, `|`, `&`, `$`, backticks, etc.).
- Arguments are passed directly to `exec.Command(command, args...)`, so shell expansion does not occur.
- Environment variable keys are validated and values are passed verbatim.

## Troubleshooting

- **Test fails immediately** — check that the command exists in the server's `PATH` and that any required Node/npm dependencies are installed globally or can be resolved by the server process user.
- **SSE URL returns 401 in browser** — pass the token via the `?token=` query parameter; browsers cannot set custom headers on `EventSource`.
- **No tools returned** — use the **Tools** button to send `tools/list`; if the server replies with an error, inspect server logs for stderr output.
