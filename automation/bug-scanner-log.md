# Bug Scanner Journal
This file is both a record and the schedule for the Daily Bug Scanner autopilot.
Each entry is appended by the autopilot after a run.
The next run derives its side from the last `- Run side:` line.
## 2026-07-25 20:10 UTC
- Run side: routing
- Baseline: `go build ./...`
- Deep check: `go test -timeout 10m ./internal/api/... ./internal/connstate/... ./internal/proxypool/...`
- Result: scanner configured, first automated run pending
- Failures: none
- Issues created: none
- Notes: Initial journal created. Next side will be `executor`.
## 2026-07-25 20:42 UTC
- Run side: executor
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/executor/... ./internal/translator/... ./internal/modalities/... ./internal/thinking/...`
- Objective result: found 1 failure
- Failure details:
- `[baseline]` `go build ./...` → `HIJ-236` (`product`)
- Parity gap: Codex Responses WebSocket executor is present in CLIProxyAPI (`internal/runtime/executor/codex_websockets_executor.go`) but missing in AxonRouter-Go; already tracked in `HIJ-152`
- Issues created: `HIJ-236`
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. `web/build/` is gitignored and not present in a fresh checkout, so `go build ./...` fails until the frontend is built. After running `cd web && npm install && npm run build`, baseline passes. Deep check (executor/translator/modalities/thinking) passed.
## 2026-07-25 20:50 UTC
- Run side: provider
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/provider/... ./internal/providercfg/... ./internal/quota/... ./internal/network/...`
- Objective result: all passed
- Failure details:
- none
- Parity gap: none
- Issues created: none
- Notes: Frontend was rebuilt (`cd web && npm install && npm run build`) before baseline because `web/build/` is gitignored.
## 2026-07-26 00:03 UTC
- Run side: auth
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/auth/... ./internal/signature/...`
- Objective result: all passed
- Failure details:
- none
- Parity gap: CLIProxyAPI `internal/signature/gemini_sanitize.go` implements `SanitizeGeminiRequestThoughtSignatures`; AxonRouter-GO lacks equivalent Gemini request thought-signature sanitization → `HIJ-266`
- Issues created: `HIJ-266`
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`), and `/workspaces/9router` was cloned fresh. Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied for routing hot-path invariants and reference priority.
## 2026-07-26 08:09 UTC
- Run side: data
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/db/... ./internal/cache/... ./internal/backup/... ./internal/background/... ./internal/logging/... ./internal/usage/...`
- Objective result: all passed
- Failure details:
- none
- Parity gap: none (no new concrete gap identified; previously-filed parity gap `HIJ-267` for log-directory size enforcement is already implemented in `internal/logging/log_dir_cleaner.go`)
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied.
## 2026-07-26 12:04 UTC
- Run side: spec-static
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `{ go vet ./...; if command -v openlore >/dev/null 2>&1; then openlore check-spec-drift --failOn=warning; fi; if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi; }`
- Objective result: all passed
- Failure details:
- none
- Parity gap: none
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. `openlore` and `staticcheck` are not installed in this environment, so the deep check effectively ran only `go vet ./...`, which passed with no output. Project `Makefile` `lint` target also uses `go vet ./...`. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied.
## 2026-07-26 20:00 UTC
- Run side: frontend
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `cd web && ([ -d node_modules ] || npm install) && timeout 10m npm run build`
- Objective result: all passed
- Failure details:
- none
- Parity gap: none (CLIProxyAPI has no frontend; OmniRoute and 9router do not have a Svelte dashboard equivalent, so no reference-backed gap to report for this side)
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. Frontend build completed successfully (`vite build`) with no errors. `npm audit` reported dependency vulnerabilities, but these were not treated as product failures under the scanner's evidence rules. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied.
## 2026-07-27 12:31 UTC
- Run side: routing
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/api/... ./internal/connstate/... ./internal/proxypool/...`
- Objective result: environment issue (test timeouts caused by SQLite fsync latency)
- Failure details:
  - `[routing/proxypool]` `go test -timeout 3m ./internal/connstate/... ./internal/proxypool/...` → `panic: test timed out after 3m0s` at `internal/db/migrations.go:833` (`environment`)
  - `[routing/api]` `go test -timeout 10m ./internal/api/... ./internal/connstate/... ./internal/proxypool/...` → `panic: test timed out after 10m0s` at `internal/db/migrations.go:833` (`environment`)
- Parity gap: none (no concrete reference-backed gap identified; parity comparison against CLIProxyAPI/9router/OmniRoute was not blocked, and none of the timeout-affected routing subsystems show a proven missing behavior)
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. The workspace was cloned fresh into the run directory. Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied. All timeouts share the same stuck goroutine inside `RunMigrations` while inserting seeded `model_pricing` rows; the top of the stack is `modernc.org/libc.Xfsync` / `syscall.Syscall(0x4a, ...)` (fsync), indicating the test DB is blocked on filesystem sync rather than on a product logic bug. This is logged as an environment/runtime issue, not a product issue. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`).

## 2026-07-27 16:07 UTC
- Run side: executor
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/executor/... ./internal/translator/... ./internal/modalities/... ./internal/thinking/...`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: none (Codex multi-agent v2 optimizer and WebSocket Responses executor gaps are already tracked as HIJ-438 / HIJ-449 / HIJ-445)
- Issues created: none
- Notes: Go binary was not on PATH; used /usr/local/go/bin/go. Baseline and executor/translator/modalities/thinking tests passed. References updated (CLIProxyAPI, OmniRoute, 9router). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied. Spurious Biome diagnostics appeared in the captured output but did not affect the exit code; no Go test failure was produced.

## 2026-07-27 20:08 UTC
- Run side: provider
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/provider/... ./internal/providercfg/... ./internal/quota/... ./internal/network/...`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: CLIProxyAPI exposes `POST /reset-quota` (`internal/api/server_management.go:77` / `internal/api/handlers/management/quota.go:26-58`) to clear quota/cooldown state for one auth index; AxonRouter-GO admin quota handler only has list, summary, and refresh endpoints, with no reset equivalent → `HIJ-911`
- Issues created: `HIJ-911`
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. Frontend already built (`web/build` present). References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied.

## 2026-07-28 00:07 UTC
- Run side: auth
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/auth/... ./internal/signature/...`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: CLIProxyAPI exposes Claude/Gemini thinking-signature validators (`ValidateClaudeThinkingSignatures`, `InspectClaudeDoubleLayerSignature`, `InspectClaudeSingleLayerSignature`, `ValidateGeminiThoughtSignatures`, `ValidateGeminiFunctionCallPairing`) in `internal/signature` that AxonRouter-Go lacks → `HIJ-924`
- Issues created: `HIJ-924`
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. Frontend already built (`web/build` present). References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied. Deep check ran clean with `-count=1` (prior cached run also passed).

## 2026-07-28 04:11 UTC
- Run side: data
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/db/... ./internal/cache/... ./internal/backup/... ./internal/background/... ./internal/logging/... ./internal/usage/...`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: none (data/cache/background/logging/usage subsystems compared against CLIProxyAPI internal/store/cache/logging and OmniRoute usage tracking; no new concrete, reference-backed gap identified)
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. Frontend already built (`web/build` present). References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied. Data-side test packages completed: `internal/db` (159.105s), `internal/cache` (0.452s), `internal/backup` (50.367s), `internal/background` (11.953s), `internal/logging` (0.011s), `internal/usage` (188.696s).

## 2026-07-28 08:04 UTC
- Run side: spec-static
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `{ go vet ./...; if command -v openlore >/dev/null 2>&1; then openlore check-spec-drift --failOn=warning; fi; if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi; }`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: none
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. `openlore` and `staticcheck` are not installed in this environment, so the deep check effectively ran only `go vet ./...`, which produced no output and exited 0. CLIProxyAPI `make lint` also uses only `go vet` (`Makefile:117-119`), so there is no reference-backed gap for openlore/staticcheck tooling. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied.

## 2026-07-28 20:05 UTC
- Run side: frontend
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `cd web && ([ -d node_modules ] || npm install) && timeout 10m npm run build`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: none
- Issues created: none
- Notes: Go binary was not on `PATH`; used `/usr/local/go/bin/go`. Fresh checkout lacks `web/build/` because it is gitignored, so the initial `go build ./...` failed with `web/embed.go:9:12: pattern all:build: no matching files found`. After running the deep-check frontend build (`npm install && npm run build`), the retry of `go build ./...` passed. References updated (`/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`, `/workspaces/9router`). CLIProxyAPI has no frontend equivalent, so no reference-backed parity gap was reported for the Svelte dashboard build. Repo-specific addendum `automation/bug-scanner-instructions.repo.md` applied.
