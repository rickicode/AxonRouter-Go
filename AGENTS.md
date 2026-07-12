# AGENTS.md — AxonRouter-Go Project Rules

## Anti-Hallucination Rule — DATA FIRST (CRITICAL)

**NO GUESSING. EVERYTHING MUST BE BACKED BY DATA.**

### Core Principles
1. **DATA = real evidence** — API responses, code, logs, screenshots, terminal output.
2. **ASSUMPTION = hallucination** — if no data exists, do not assume.
3. **VERIFY before claiming** — check first, then speak.
4. **Model names, URLs, endpoints, behavior** — ALL must be verified from the codebase or live API.

### Workflow (MUST follow)
1. Search the codebase first (`grep`, `read`, `lsp`).
2. If missing, check reference codebases (`OmniRoute`, `CLIProxyAPI`, `AMRouter`).
3. If still missing, use web search.
4. If still missing, say: **"I don't know; no data available."**

### Examples
- ❌ Wrong: "Mimo has balance tracking" — not checked against code.
- ❌ Wrong: "Codex quota resets weekly" — not verified.
- ✅ Right: "OmniRoute quotaCache.ts line 15 refreshes every 1 minute."
- ✅ Right: "Endpoint is at router.go line 219."

## Code Tool Policy (CRITICAL)

- Unknown implementation, behavior, or code location → use **Semble**.
- Known class, function, method, type, or symbol → use **Serena**.
- Exact text, regex, or exhaustive occurrence search → use **`rg`**.
- Do not use **`rg`** to discover how a feature is implemented when Semble is available.

When **Serena** is unavailable, fall back to `lsp`, `ast_grep`, and `ast_edit` as the semantic-equivalent tools for code navigation and editing.

### Serena Configuration (Project-Specific)

This project uses **Go** for the backend and **Svelte/TypeScript** for the frontend. Before relying on Serena, ensure `/workspaces/AxonRouter-GO/.serena/project.yml` lists both languages:

```yaml
languages:
  - go
  - svelte
```

If only `svelte` is listed, Go symbol queries will time out because `gopls` is not started.

### Semble — discovery

Use Semble first when the relevant file, symbol, or behavior is not yet known. Use it for:
- Natural-language and semantic code searches.
- Finding features, flows, retry logic, error handling, and relevant modules.
- Exploring unfamiliar or large repositories.
- Narrowing the search area before reading files or using symbol-level tools.

### Serena / `lsp` / `ast_grep` — semantic navigation and editing

Use Serena when it is available for symbol-level work:
- Finding classes, functions, methods, interfaces, types, and constants.
- Finding references, callers, declarations, and implementations.
- Renaming symbols, replacing whole method/function bodies, and cross-file refactoring.

When Serena is unavailable, use this harness's built-in tools in order:
1. `lsp` for go-to-definition, references, rename, code actions, and diagnostics.
2. `ast_grep` for structural pattern discovery.
3. `ast_edit` for safe codemods and structural rewrites.
4. `edit` for small localized changes.

### `rg` / `grep` — textual/regex search

Use `rg` (or this harness's built-in `grep` tool) only when textual matching is the correct operation:
- Exact strings, regular expressions, and literal patterns.
- Error messages, logs, comments, TODOs, URLs, and environment-variable names.
- Configuration, documentation, templates, scripts, and non-code files.
- Verifying that an old name or value has been completely removed.
- Cases where Semble, Serena, `lsp`, or `ast_grep` cannot locate the target.

Do not use `rg`/`grep` as the default tool for semantic code exploration.

### Editing

Use Serena for structural source-code edits when available.
Use built-in `edit` (or `write` for new files / full-file replacement) for:
- Small localized code changes.
- Documentation and non-code files.
- Creating new files.
- Cases where Serena, `lsp`, or `ast_edit` cannot modify the target.

If a textual source-code edit fails:
1. Re-read the current file and retry once with fresh context.
2. If the second attempt fails, stop textual retries and switch to `ast_edit` or Serena.
3. Fall back to textual editing only if no semantic tool can modify the target.

### Preferred workflow

For an unknown implementation:
1. **Semble** → discover relevant code.
2. **Serena / `lsp` / `ast_grep`** → locate exact symbols and references.
3. **Serena / `ast_edit` / `edit`** → make the change.
4. **`ast_grep` or `rg`/`grep`** → verify removals.
5. Inspect the diff and run relevant diagnostics, tests, linting, type checks, or builds.

### Verification

After every edit:
- Inspect the diff and confirm only intended locations changed.
- Check for duplicated, truncated, or misplaced code.
- Run `go build ./...` and `go test ./...` (or the relevant package tests).
- For frontend changes, run `npm run build` in `web/` until zero warnings.

## Multi-Codebase Comparison Rule (CRITICAL)
When CLIProxyAPI, AxonRouter, and OmniRoute implement the same subsystem:
1. Read ALL three implementations.
2. Compare: which is most efficient, complete, and stable?
3. Pick the best one. Do not mix unrelated pieces.
4. Quote the source and explain the choice.

### Examples
- ✅ "Quota detection uses OmniRoute's `getUsageForProvider` because it supports 8 providers. AxonRouter only detects from error text."
- ✅ "Circuit breaker uses AxonRouter because its state machine (CLOSED→OPEN→HALF_OPEN) is clearest."

## Reference Codebases

| Codebase | Path | Language | Strengths |
|----------|------|----------|-----------|
| CLIProxyAPI | `/workspaces/CLIProxyAPI` | Go | Translator, auth, executor (production-tested) |
| AxonRouter | `/workspaces/AxonRouter` | TypeScript | Combo system, dashboard, usage tracking |
| OmniRoute | `/workspaces/OmniRoute` | TypeScript | 231 providers, quota cache, policy engine |

## Tech Stack (Fixed)

- Backend: Go + Gin + SQLite
- Frontend: Svelte (embedded via `go:embed`)
- CLI: Minimal — service management + status only
- Config: SQLite (not YAML/file-based)

## Provider Naming

- Prefix = provider identifier: `cx/`, `openai/`, `mimo/`, `ag/`, `kiro/`, etc.
- Codex ≠ OpenAI: `cx/gpt-5.4` is not `openai/gpt-5.4`.
- Custom provider: user-given name becomes the prefix (e.g., `9router/gpt-4o`).

## Scale Assumptions

- 100–1000+ connections per provider.
- Routing must be <1ms regardless of connection count.
- Pre-computed eligible list for O(1) routing.
- Dashboard pagination is mandatory.

## Execution & Build Rules

1. **Always commit** once the work is stable and tests pass.
2. **Zero warnings** on `npm run build`.

## UI/UX Toast Notifications (Required)

Every user action that triggers a backend response MUST use `svelte-sonner`.

- Import: `import { toast } from 'svelte-sonner'`.
- `<Toaster />` is already in `App.svelte`.
- Use specific messages:
  - `toast.success('Connection reset to ready')`
  - `toast.error('Test failed: ' + err.message)`
  - `toast.info('Syncing models...')`
- Never use `alert()` or silent failures.

## Page Layout Convention (Required)

All dashboard pages MUST use the same layout pattern.

### Outer wrapper
```svelte
<div class="flex flex-1 flex-col gap-6 p-6">
```

### Heading pattern
```svelte
<div class="space-y-1">
  <h1 class="text-display-lg">Page Title.</h1>
  <p class="text-body-sm text-muted-foreground">Description text</p>
</div>
```

### Card surfaces
- `bg-card` (`#18181b`) for card backgrounds
- `shadow-card` / `shadow-elevated` for elevation
- `rounded-xl` (12px) for radius
- `border-border` for borders
- NEVER use raw hex like `bg-[#18181b]` — use Tailwind tokens

### Typography tokens
- `text-display-lg` — page headings (32px, 600, -1.28px tracking)
- `text-display-md` — section headings (24px, 600)
- `text-body-sm` — body text (14px, 400)
- `text-body-sm-strong` — bold body (14px, 500)
- `text-caption` — small labels (12px, 400)
- `text-caption-mono` — mono labels (12px, mono)

### Buttons
```svelte
<Button variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer">
```
NEVER create custom button styling.

### Reference pages
- `Providers.svelte` — gold standard layout
- `Combos.svelte` — card grid pattern
- `Logs.svelte` — table + filters pattern

## Session Workflow (Todo + Git Hygiene)

### 1. Todo Tracking (Required)
For any multi-step or multi-file task, always maintain a `todo` list:
1. Initialize the list before starting work.
2. Mark items `in_progress` while working.
3. Mark items `done` when finished.
4. Do not delete or ignore the list until the work is complete.

### 2. Check Git Status Before Editing
Before touching any file, run:
```bash
git status --short
# or
git diff --name-only
```
Files with **M/A/D** status that you did not change in this session are code from another session — **hands-off**.

### 3. If You Must Touch Another Session's Code
If your new code truly depends on a file touched by another session:
- Edit only as needed / as efficiently as needed.
- Add an inline comment at the touched location:
  ```go
  // NOTE: <short reason this code was touched>
  ```
- Include in the commit message: `NOTE: <reason>`.

### 5. Verify Before Commit
Run:
```bash
git diff --cached --stat
```
Make sure you only commit changes you intended. Do not include unrelated changes from other sessions.

## Local Real Testing (Without Disturbing the Main Gateway)

When you need to run an actual instance for manual or smoke testing after building:

```bash
make run-dev
```

This builds the binary and starts a **dev server** on the alternate port (`3788` by default) with an **isolated data directory** at `/tmp/axon-dev`.

### Why `run-dev`, not `make run`?
- `make run` starts the server on the **main port (3777)** and uses the default data dir. This is the live AI gateway.
- `make run-dev` leaves port 3777 and the default data dir untouched, so the main gateway keeps running normally while you rebuild or test.

### How it works
- `AXON_PORT` is set to `DEV_PORT` (default `3788`) instead of `3777`.
- `AXON_DATA_DIR` is set to `/tmp/axon-dev`, so the dev instance uses its own SQLite database and PID file.
- The `kill-dev-port` target clears the dev port if a previous dev server is still running, but it never touches port 3777.
