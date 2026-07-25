# AxonRouter-GO Bug Scanner Addendum

These rules are **additional** to the universal Bug Scanner instructions. If a rule below contradicts the universal instructions, this file wins for AxonRouter-GO.

## 1. Routing hot-path invariants (do not touch without human confirmation)
The connection-selection path in `internal/api/handlers/v1/handler.go` is heavily optimized. When investigating failures there, preserve every invariant listed below and never propose changing them:
- Eligibility snapshot is lock-free (`atomic.Value` in `internal/connstate/eligibility.go`).
- Rebuilds are coalesced into a 50 ms window.
- Hot path samples at most 10 eligible candidates (`pickMaxAttempts`).
- Pre-sorted `*ConnectionState` pointers (`ByPrefixState`) avoid repeated `store.Get` lookups.
- Round-robin counter is keyed by `providerID + "\x00" + modelID` via `providercfg.NextRoundRobinIndex`.
- `tryPickConnection` uses a single captured `now`; do not add extra `time.Now()` calls.
- `ExhaustionCache` and the credential cache in `Handler.conns` use `sync.Map` — no global `sync.RWMutex` on the hot path.
- Cold disk loads use `singleflight` through `providercfg.Manager.Get`.
- `ConnectionState.lastUsedAt` is an in-memory-only `atomic.Int64` unix-nano tiebreaker, not persisted.

If you suspect a routing bug, the issue must cite **benchmarks, exact latency numbers, or a concrete failure that respects the above invariants**.

## 2. Provider and model rules
- Provider prefix **is** the identifier (`cx/`, `openai/`, `mimo/`, `ag/`, `kiro/`, etc.). `cx/gpt-5.4` is not `openai/gpt-5.4`.
- Custom provider prefix = the user-given name (e.g. `9router/gpt-4o`).
- `internal/usage/pricing.go` keys pricing by the model ID **after** the provider prefix. There is no per-provider pricing.
- Seeded prices must be for canonical/main models only, with representative public rates, and every seeded row must have non-zero input+output price.
- Do not propose adding a new provider or model unless it is already confirmed to exist in the code.

## 3. Security hard line
- **Never** create issues or propose code that implements MITM, traffic interception, transparent proxies, TLS termination, certificate bypass, or any man-in-the-middle handler for providers.
- All provider integrations must use documented official HTTP/HTTPS endpoints and explicit API keys.

## 4. Reference priority for parity checks
**AxonRouter-GO must always keep these references fresh and compare its current side against them for parity gaps.** These paths are already listed in `automation/bug-scanner-config.json` under `references`, so switching to them is allowed by the universal scope rule.
- **CLIProxyAPI** (`/workspaces/CLIProxyAPI`) — primary reference for Go backend, auth, executor, and translator patterns.
- **9router** (`/workspaces/9router`) — lightweight reference for custom provider behavior, CLI patterns, and modular routing ideas.
- **OmniRoute** (`/workspaces/OmniRoute`) — secondary, use only for provider/quota/policy patterns that are explicitly applicable.

Rules:
- Compare **only the current side** and cite exact source lines or URLs.
- A parity issue must ask for something the reference already implements and AxonRouter clearly lacks. **Do not ask for new features that no reference implements.**
- Do not copy entire subsystems from any reference; only flag concrete, evidence-backed gaps.

## 5. Versioning and release
- Canonical version is in `internal/version/VERSION`. Never edit it by hand; use `make set-version v=X.Y.Z`.
- If a bug scan surfaces a versioning/release issue, include `CHANGELOG.md` sync and README marker block status in the evidence.

## 6. UI conventions (for front-end bugs only)
- All new or fixed UI flows must use `svelte-sonner` toasts, never `alert()`.
- Page layout must follow the outer wrapper + heading + card pattern documented in the project rules.
- Use Tailwind tokens (`bg-card`, `shadow-card`, `rounded-xl`, `border-border`, etc.), never raw hex classes like `bg-[#18181b]`.

## 7. Test policy for this repo
- Backend/Go and shared package fixes should include regression tests, but the **scanner must not create tests itself**.
- Frontend unit tests must **never** be added.
- If a static check (vet, staticcheck, Go build) surfaces a failure in `internal/api/handlers/v1/handler.go` or `internal/connstate/`, treat it as high-priority product evidence.

## 8. Hard feature-freeze for AxonRouter-GO
- The scanner may only report **existing** bugs.
- **Never** propose adding a feature, endpoint, provider, model, UI page, or capability that does not already exist in the codebase, unless a human explicitly confirms.
- A parity gap is the only exception: if CLIProxyAPI or 9router already has a proven behavior that AxonRouter lacks in the same subsystem, it may be reported as a parity issue with evidence.
