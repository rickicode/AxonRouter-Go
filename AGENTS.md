# AGENTS.md — AxonRouter-Go Project Rules

## Anti-Hallucination Rule — DATA FIRST (CRITICAL)

**NO GUESSING. EVERYTHING MUST BE BACKED BY DATA.**

### Core Principles
1. **DATA = real evidence** — API responses, code, logs, screenshots, terminal output.
2. **ASSUMPTION = hallucination** — if no data exists, do not assume.
3. **VERIFY before claiming** — check first, then speak.
4. **Model names, URLs, endpoints, behavior** — ALL must be verified from the codebase or live API.
5. **ALWAYS use Semble + `rg` first** — for implementation questions, model names, endpoints, or any code context, search with Semble (discovery) and `rg` (exact/literal verification) before answering.

### Workflow (MUST follow)
1. **Always use Semble first** when the file, symbol, or behavior is not yet known; it is the primary discovery tool for this project.
2. Use **`rg`** (or the built-in `grep` tool) for exact text/regex searches, error messages, TODOs, URLs, and for verifying an old name/value has been completely removed.
3. Verify any code, model name, URL, endpoint, or behavior against actual source code or a live API response before claiming it.
4. If missing in this repo, check reference codebases (`OmniRoute`, `CLIProxyAPI`, `AMRouter`) with the same Semble/`rg` discipline.
5. If still missing, use web search.
6. If still missing, say: **"I don't know; no data available."**

**No guessing.** When there is no supporting data, respond "I don't know" rather than filling the gap with assumptions.

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

## Prohibited: MITM, Traffic Interception, and Transparent Proxies (CRITICAL)

NEVER implement, ship, or configure any form of **MITM**, **traffic intercept**, **IDE transparent proxy**, **TLS termination**, **certificate bypass**, or **man-in-the-middle handler** for Kiro, Codex, OpenAI, Mimo, or any other provider. This applies to any code, configuration, documentation, or workaround that sits between a user, IDE, plugin, or command-line tool and an upstream provider's official endpoint in order to inspect, modify, reroute, or replay encrypted traffic.

This is non-negotiable because:
- It violates provider Terms of Service and applicable computer-fraud / privacy laws.
- It breaks TLS trust for users and upstream services, creating a severe security liability.
- It introduces unsustainable maintenance: certificate pinning, protocol drift, and IDE/version-specific proxy behavior become the project's responsibility.
- It makes AxonRouter liable for intercepted content, credentials, and telemetry.

All provider integrations MUST use documented, official HTTP/HTTPS endpoints and explicit API keys supplied by the operator. If a provider's official API does not support a feature, that feature is out of scope — do not work around it via interception.

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
- CLI: Planned — service management + status only (not yet shipped)
- Config: SQLite (not YAML/file-based)

## Provider Naming

- Prefix = provider identifier: `cx/`, `openai/`, `mimo/`, `ag/`, `kiro/`, etc.
- Codex ≠ OpenAI: `cx/gpt-5.4` is not `openai/gpt-5.4`.
- Custom provider: user-given name becomes the prefix (e.g., `9router/gpt-4o`).

## Model Pricing Seed Rule

- `model_pricing` is keyed by the **model ID after the provider prefix** (`internal/usage/pricing.go` strips everything before the first `/`). There is **no per-provider pricing**.
- Seed only **canonical/main models** with representative (average) public rates. Do not try to seed every alias returned by every provider.
- Every seeded row must have a non-zero input+output price; `validateSeedPricing` rejects `$0` rows.
- Free/subscription providers without a per-token rate (e.g., Pollinations, Copilot, OpenCode Free) are intentionally left out of the seed; operators can add custom rows through the admin **Model Pricing** UI.
- When adding a new provider, look up the public price for its flagship models and seed **only the model part** that `GetPricing` will actually look up.

## Scale Assumptions

- 100–1000+ connections per provider.
- Routing must be <1ms regardless of connection count.
- Pre-computed eligible list for O(1) routing.
- Dashboard pagination is mandatory.

## Routing Hot Path Implementation Notes

The connection-selection path in `internal/api/handlers/v1/handler.go` is heavily optimized. When touching routing code, preserve these invariants:

1. **Eligibility snapshot is lock-free** — stored in `atomic.Value` (`internal/connstate/eligibility.go`). Rebuilds are coalesced into a 50ms window to avoid O(N) spikes under bursty failovers.
2. **Hot path is bounded** — `getConnection` samples at most `pickMaxAttempts = 10` eligible candidates before falling back; the snapshot also stores pre-sorted `*ConnectionState` pointers (`ByPrefixState`) to avoid repeated `store.Get` lookups.
3. **Round-robin is per `provider/model`** — `providercfg.NextRoundRobinIndex` keys its atomic counter by `providerID + "\x00" + modelID` so models rotate independently.
4. **Avoid repeated `time.Now()`** — `tryPickConnection` captures one `now` value and passes it to cooldown/exhaustion checks. Add `_At(now)` variants instead of adding new clock reads.
5. **Read-heavy caches use `sync.Map`** — `ExhaustionCache` and the connection credential cache in `Handler.conns` are `sync.Map`; don't introduce `sync.RWMutex` guarding a global map on the hot path.
6. **Resolve `RoutingMode` once per request** — pass the resolved `providercfg.RoutingMode` down the call chain; do not re-query the provider-config manager inside loops or log statements.
7. **Cold disk loads use `singleflight`** — `providercfg.Manager.Get` collapses concurrent first-time reads for the same provider JSON to a single `os.ReadFile`.
8. **Recency tiebreaker is in-memory only** — `ConnectionState.lastUsedAt` is an `atomic.Int64` (unix-nano) used as a secondary sort key; it is not persisted to SQLite.

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

## Pull Request Labels (Required)

When creating a pull request (via `gh pr create` or any Hive merge), always attach the label:

```
auto-merge
```

If the label does not exist in the repository yet, create it first with:

```bash
gh label create "auto-merge" --color 0366D6 --description "Auto-enable PR merge when checks pass"
```

This applies to every PR in this Hive-managed repository.

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
- `HOME` is set to `/tmp/axon-dev`, so the dev instance uses its own SQLite database and PID file under `/tmp/axon-dev/axonrouter`.
- The `kill-dev-port` target clears the dev port if a previous dev server is still running, but it never touches port 3777.

## Hive Worktree Cleanup Rule (CRITICAL)

When using Hive-managed feature worktrees, **do not let merged worktrees accumulate indefinitely**. If there are **10 or more Hive worktrees whose branches have already been merged into `master`**, those worktrees **must be deleted** to keep the workspace clean and avoid disk/branch pollution.

### How to check
```bash
git branch -a | grep 'hive/' | wc -l
```

### How to delete a merged worktree
```bash
git worktree remove <path-to-worktree>
git branch -D <branch-name>
```

The `git worktree remove` path is typically `.hive/.worktrees/<feature>/<task-folder>`.

---

## Versioning & Changelog

AxonRouter-Go uses a single-file versioning system so that every release is consistent across binary, dashboard, GitHub Releases, and `CHANGELOG.md`.

### 1. Single Source of Truth
- The canonical version lives in **`internal/version/VERSION`**.
- Never change the version directly in `web/package.json`, dashboard code, banner strings, or Git tags.
- All tooling reads `internal/version/VERSION`; derived files are updated automatically.

### 2. Changing the Version
- Always use the Makefile targets; do **not** edit `internal/version/VERSION` by hand as the only step.
- Bump and sync version across files:
  ```bash
  make set-version v=0.3.1
  ```
- Create a release (commits, tags, and pushes):
  ```bash
  make release v=0.3.1
  ```
  `make release` will fail if the working tree is dirty or `CHANGELOG.md` has no entries under `## [Unreleased]`.

### 3. CHANGELOG.md Is Mandatory
- Every release **must** update `CHANGELOG.md`.
- Add entries under `## [Unreleased]` as you build features/fixes.
- Use categories from [Keep a Changelog](https://keepachangelog.com): `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`.
- During release, the `## [Unreleased]` section is moved into a new `## [X.Y.Z] - YYYY-MM-DD` section.
- GitHub Releases uses `scripts/prepare-release-notes.js` to pull the matching section from `CHANGELOG.md`, falling back to the latest git commit message when no section exists.
### 4. Git Tags and GitHub Actions
- Release tags must match `v<VERSION>` (e.g. `internal/version/VERSION` = `0.3.1` → tag `v0.3.1`).
- The `.github/workflows/release.yml` workflow:
  1. Validates that the pushed tag matches `internal/version/VERSION`.
  2. Builds the frontend and backend via Makefile targets.
  3. Prepares release notes using `scripts/prepare-release-notes.js`:
     - If a section for the current version exists in `CHANGELOG.md`, it uses that.
     - If no section exists, it falls back to the latest git commit message.
  4. Publishes the release notes and binaries to GitHub Releases with a clear `Release vX.Y.Z` title.

### 5. README.md Changelog Sync
- `README.md` contains a `<!-- LATEST_CHANGELOG_START -->` / `<!-- LATEST_CHANGELOG_END -->` marker block.
- `scripts/update-readme.js` injects the latest release section from `CHANGELOG.md` into that block.
- `make release` calls `update-readme.js` automatically before committing.
- CI verifies README is in sync by running `node scripts/update-readme.js --check`; a stale README fails the build.

### 6. Where Version Is Exposed
- **Startup banner**: printed by `cmd/server/main.go` using `internal/version`.
- **Health endpoint**: `GET /api/admin/health` returns `{ "status": "...", "version": "0.3.1" }`.
- **Dashboard sidebar**: reads `version` from the health response and links to the GitHub `CHANGELOG.md`.

### 7. Release Procedure — Agent Must Follow
When the user asks to "release", "buat release", "update release", or any equivalent, do not just run the Makefile blindly. Follow this checklist.

#### 7.1 Do not release a dirty working tree
- If `git status --short` shows any `M`/`A`/`D` files, stage and commit them first with an accurate message.
- If you see files you did **not** modify in this session, ask the user before committing them.

#### 7.2 Verify before release
Run these and do not proceed until they pass:
```bash
go build ./...
go test ./...
cd web && npm run build   # zero warnings
cd web && npm run test
```

#### 7.3 Confirm the changelog
- `CHANGELOG.md` must have entries under `## [Unreleased]`.
- If `## [Unreleased]` is empty, ask the user what changed instead of fabricating entries.

#### 7.4 Create the release
Use the exact version the user asked for. If no version was specified, ask.
```bash
make release v=X.Y.Z
```

#### 7.5 Verify the release artifacts
- Local tag: `git tag --list 'vX.Y.Z'`
- Remote tag: `git ls-remote --tags origin vX.Y.Z`
- GitHub Actions release workflow is triggered by the tag.

#### 7.6 If recreating an existing release
Sometimes the user deletes a release and wants the same version again. Do this first:
1. Delete remote tag: `git push --delete origin vX.Y.Z`
2. Delete local tag: `git tag -d vX.Y.Z`
3. Delete GitHub release: `gh release delete vX.Y.Z --yes`
4. If the release commit is already on `master`, revert it and push:
   ```bash
   git revert <release-commit-sha>
   git push origin master
   ```
5. Then follow 7.1–7.6.
