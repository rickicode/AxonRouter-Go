# Task: 02-reorder-settings-tabs-and-integrate-runtimesettings

## Feature: runtime-settings-total-redesign

## Dependencies

- **1. Create RuntimeSettings component** (01-create-runtimesettings-component)

## Plan Section

### 2. Reorder Settings tabs and integrate RuntimeSettings
**Depends on**: 1  
**Files:**
- Modify: `web/src/pages/Settings.svelte`

**What to do:**
- Change tab order to `Runtime`, `Security`, `_https_`.
- Set default tab value to `runtime`.
- Remove runtime state, helper functions, and UI loop from `Settings.svelte`.
- Import and render `<RuntimeSettings />` inside `Tabs.Content value="runtime"`.
- Keep Change Password and Data Management in Security, and `<HttpsSettings />` in HTTPS.

**Must NOT do:**
- Do not alter ChangePasswordCard or HttpsSettings behavior.

**Verify:**
- `cd web && npm run build` → zero warnings.

---

## Task Type

modification
