# Task: 02-replace-configbutton-enabledisable-toggles-with-switch

## Feature: shadcn-switch-cleanup

## Dependencies

- **1. Replace table StatusBadge active toggles** (01-replace-table-statusbadge-active-toggles)

## Plan Section

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
