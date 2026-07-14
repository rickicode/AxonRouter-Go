# Task: 01-create-runtimesettings-component

## Feature: runtime-settings-total-redesign

## Dependencies

_None_

## Plan Section

### 1. Create RuntimeSettings component
**Depends on**: none  
**Files:**
- Create: `web/src/lib/components/RuntimeSettings.svelte`

**What to do:**
- Fetch settings via `settingsApi.list()` on mount.
- Only show keys defined in `settingMeta` (Background Jobs, Routing, Logging). Drop `Other`.
- Add category filter pills (`All`, `Background Jobs`, `Routing`, `Logging`).
- Add search input.
- Render as a clean list/table (`div` based): columns Setting, Description, Current value, Actions.
- Inline edit: clicking the value or the Edit button swaps the value cell into an Input + Save/Cancel icon buttons.
- Save calls `settingsApi.update`, updates local state, shows toast.

**Must NOT do:**
- Do not display unknown or non-runtime keys.
- Do not use the old category-card layout.

**Verify:**
- `cd web && npm run build` → zero warnings.

---

## Task Type

greenfield
