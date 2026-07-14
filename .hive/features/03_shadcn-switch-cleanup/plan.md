# Replace legacy toggles with shadcn Switch

## Discovery
- shadcn `<Switch>` is already installed at `web/src/lib/components/ui/switch` and used in forms/modals (`Optimization`, `HttpsSettings`, `ProxyPools` bulk/group modals, `Combos` create modal).
- Legacy switch-like UIs still in use:
  - **ProxyPools.svelte**: `StatusBadge` On/Off clickable toggles in the pools table and groups table (lines 687, 761).
  - **Combos.svelte**: `StatusBadge` On/Off clickable toggle in combos table (line 176).
  - **CLITools.svelte**: native `<input type="checkbox">` for "Auto-discover models" (line 507).
  - **APIKeys.svelte**: Enable / Disable button in table actions (line 186).
  - **ProxyPoolDetail.svelte**: Enable / Disable button in actions card (line 259).
  - **ComboDetail.svelte**: Enable / Disable button in header actions (line 164).
- Import alias is `$lib/components/ui/switch` (matching `components.json` / `tsconfig.json`), not `@/components/ui/switch`.

## Design Summary
Replace every binary active/enable toggle that is rendered as a clickable `StatusBadge`, button, or checkbox with the shadcn `<Switch>` component. Keep multi-select toggles (service kinds, model picker) and row-selection checkboxes unchanged because they are not boolean on/off switches.

## Non-Goals
- Do NOT convert row-selection checkboxes in tables.
- Do NOT convert accordion/collapse toggles (Providers page).
- Do NOT convert password-visibility eye toggles.
- Do NOT add new API endpoints or change data models.

## Ghost Diffs Considered
- Keep Enable/Disable buttons as text buttons — rejected because user explicitly asked for shadcn Switch UI.
- Replace with a custom Switch wrapper — rejected because shadcn Switch already exists and is used elsewhere.

## Tasks

### 1. Replace table StatusBadge active toggles
**Depends on:** none  
**Files:**
- `web/src/pages/ProxyPools.svelte` (line 687, 761)
- `web/src/pages/Combos.svelte` (line 176)

**What:**
- Change pool/group/combo active `StatusBadge` On/Off into `<Switch checked={...} onCheckedChange={() => toggle...()} />`.
- Center the switch in the table cell.
- Keep health/error status badges unchanged.

**Verify:**
- `npm run build` in `web/` exits 0 with no warnings.
- `go build ./...` passes.

### 2. Replace config/button Enable/Disable toggles with Switch
**Depends on:** 1  
**Files:**
- `web/src/pages/CLITools.svelte` (line 507)
- `web/src/pages/APIKeys.svelte` (line 186)
- `web/src/pages/ProxyPoolDetail.svelte` (line 259)
- `web/src/pages/ComboDetail.svelte` (line 164)

**What:**
- Convert Auto-discover checkbox to `<Switch id="auto-discovery" bind:checked={sel.useDiscovery}>` with a `Label`.
- Replace Enable/Disable action buttons with `<Switch checked={...} onCheckedChange={handleToggle} disabled={!!actionLoading} />`.
- Preserve existing loading/disabled behavior.
- Add `Switch` import where missing.

**Verify:**
- `npm run build` in `web/` exits 0 with no warnings.
- `go build ./...` passes.

### 3. Build verification and commit
**Depends on:** 2  
**Files:** none  

**What:**
- Run full verification.
- Stage only unintended files touched in this session.
- Commit with concise message.

**Verify:**
- `git status --short` shows only expected modified files.
- `go build ./...` and `npm run build` still pass.
