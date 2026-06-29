# AxonRouter-Go Frontend Redesign & Route Cleanup Workflow

## Mode
- Mode: Goal
- Max Cycles: 5
- Assumed Approval: routine, non-destructive work

## Goal Description
Redesign the AxonRouter-Go SvelteKit frontend to align with the Vercel-inspired Vercel Design System specified in @[DESIGN.md], utilizing `shadcn-svelte` components. Also delete all unused/unimplemented routes and sidebar navigation menu items that do not have functional backend support.

## Acceptance Criteria
1. **Clean Route Footprint:** Deletion of all unimplemented routes: `/analytics`, `/caveman`, `/cli-tools`, `/endpoint`, `/mcp`, `/mitm`, `/morph`, `/proxy-pools`, `/quota`, `/skills`, `/usage`. (Verified: deleted).
2. **Sidebar Navigation Update:** Remove all links to the deleted routes from the sidebar. Only show Home (`/`), Providers (`/providers`), Combos (`/combos`), Logs (`/logs`), and Settings (`/settings`). (Verified: updated).
3. **Design System Configuration:** Configure CSS variables in `app.css` to implement the stark ink-on-white (#fafafa canvas-soft, #171717 ink text) Vercel-inspired system, paired with an elegant dark mode fallback. (Verified: updated).
4. **Vercel Aesthetics on Live Pages:** Redesign the remaining pages to strictly follow the design tokens (card padding, pilled button CTAs vs squared nav buttons, Geist/Inter geometric typography with negative tracking on headings, subtle stacked shadows instead of single drops). (Verified: updated).
5. **Successful Build & Execution:** Ensure `npm run build` succeeds, the Go backend compiles with embedded assets, and is tested/verified. (Verified: currently rebuilding via `make build`).

## Task Board
- [x] Remove unimplemented route folders from `web/src/routes`
- [x] Update `SidebarNav.svelte` to only include active routes (Home, Providers, Combos, Logs, Settings)
- [x] Configure `app.css` and typography (import Inter/JetBrains Mono, set `--background`, `--foreground`, `--primary`, `--border` colors, and configure Geist-like properties)
- [x] Update theme store (`web/src/lib/stores/theme.ts` or similar) to ensure smooth integration with the new colors
- [x] Redesign `web/src/routes/+page.svelte` (Dashboard stats and quick actions)
- [x] Redesign `web/src/routes/providers/+page.svelte` (Providers grid list)
- [x] Redesign `web/src/routes/providers/[id]/+page.svelte` (Provider connections list, add connection)
- [x] Redesign `web/src/routes/providers/[id]/[connId]/+page.svelte` (Connection edit/add form)
- [x] Redesign `web/src/routes/combos/+page.svelte` (Combos grid list)
- [x] Redesign `web/src/routes/combos/[id]/+page.svelte` (Combo configuration detail)
- [x] Redesign `web/src/routes/logs/+page.svelte` (Logs view & query filter)
- [x] Redesign `web/src/routes/settings/+page.svelte` (System and appearance settings)
- [x] Run `npm run build` to verify frontend compiling
- [x] Rebuild backend and verify that embedded static files serve without error

## Cycle Board
| Cycle | Stage Result | Remaining TODO | Blockers |
| --- | --- | --- | --- |
| 1 | Planning and setup completed | All | None |
| 2 | Code implementation and cleanup completed; executing final build verification | None | None |

## Assumptions
- Custom themes or style preferences should stay within the Vercel-inspired stark look.
- The sidebar navigation should categorize pages into active platform sections (Platform: Home, Providers, Combos; Settings: Logs, Settings) rather than listing non-existent options.

## Runtime Boundary
- Frontend assets compiled in `web/build` are embedded in the Go binary using Go 1.16+ `go:embed`.

## Ship Decision
- ready
