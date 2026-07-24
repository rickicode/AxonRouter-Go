---
title: Implementasi Qoder OAuth / Parity OmniRoute & 9router di AxonRouter-GO
type: task
permalink: axonrouter-go/tasks/implementasi-qoder-oauth-parity-omni-route-9router-di-axon-router-go
status: in-progress
current_step: Semua fase selesai — Fase 1 dual-mode OAuth+PAT, Fase 2 quota+validation, Fase 3 model pricing seed sudah di-commit
status: completed
started: 2026-07-24
completed: 2026-07-24
tags:
- qoder
- oauth
- provider
- omniroute
- 9router
---

## Tujuan
Meningkatkan provider Qoder di AxonRouter-GO agar mendukung OAuth dan mencapai parity fitur dengan OmniRoute & 9router.

## Current State
- AxonRouter-GO Qoder: `authType: apikey`, dual-mode executor (PAT `pt-*` → `qodercli`; lainnya → DashScope HTTP).
- Belum ada OAuth handshake, quota fetcher, PAT import UI, maupun connection validation Qoder-spesifik.

## Decisions Needed / Scope
1. OAuth flow: authorization-code (OmniRoute) vs device-code (9router, deprecated).
2. Endpoint defaults: env-only atau built-in public endpoint.
3. Dual-mode UI: satu entry Qoder dengan OAuth + PAT/API-key, atau entry terpisah.
4. Token source HTTP saat OAuth: access_token langsung vs apiKey dari userInfo.
5. Refresh behavior: disable connection on failure atau fallback ke PAT/CLI.
6. Apakah porting 9router COSY/WAF signing atau menolak karena deprecated/TOS-risk?

## Proposed Phases (lihat plan workflow qoder_oauth_parity_analysis)
1. Backend OAuth service skeleton - P1
2. Executor honor OAuth/PAT - P1
3. Provider catalog & DB category migration - P2
4. Admin PAT import - P2
5. Quota fetcher for Qoder - P2
6. Admin validation & CLI test - P3
7. Seed model pricing - P3

## Acceptance Criteria
- [ ] `qoder` support OAuth authorization-code (env-configured).
- [ ] PAT/`qodercli` tetap berfungsi seperti sekarang.
- [ ] Quota fetcher `getQoderUsage()` mengembalikan data valid.
- [ ] Semua test lama pass; test baru ditambahkan.
- [ ] Dashboard dapat memilih mode OAuth vs PAT/API-key.
- [ ] `go build ./...`, `go test ./...`, `cd web && npm run build` zero warnings.

## Reference Run
- Workflow ID: `qoder-oauth-parity-analysis-mry0y1tx-z1xy4h`
- Path: `/home/agent/.pi/workflows/projects/axonrouter-go-a0fd5b60aee8/runs/qoder-oauth-parity-analysis-mry0y1tx-z1xy4h.json`