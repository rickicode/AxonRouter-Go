# Task: 01-replace-table-statusbadge-active-toggles

## Feature: shadcn-switch-cleanup

## Dependencies

_None_

## Plan Section

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
