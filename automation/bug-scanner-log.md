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

## 2026-07-26 04:08 UTC
- Run side: data
- Baseline: `export PATH=$PATH:/usr/local/go/bin && go build ./...`
- Deep check: `export PATH=$PATH:/usr/local/go/bin && go test -timeout 10m ./internal/db/... ./internal/cache/... ./internal/backup/... ./internal/background/... ./internal/logging/... ./internal/usage/...`
- Objective result: all passed
- Failure details:
  - none
- Parity gap: CLIProxyAPI `internal/logging/log_dir_cleaner.go` enforces a max total log-directory size cap; AxonRouter-GO defines `Config.LogDir` but has no size cap or periodic cleaner → `HIJ-267`
- Issues created: `HIJ-267`
- Notes: References updated; OmniRoute fast-forwarded from `4053e23` to `f60c9fe`. Frontend build was not required because `web/build` was already present in this checkout.
