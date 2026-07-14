# Task: 07-build-https-settings-tab-ui

## Feature: 02_settings-https-tls

## Dependencies

- **6. Redesign Settings.svelte with tabs** (06-redesign-settingssvelte-with-tabs)

## Plan Section

### 7. Build HTTPS settings tab UI
**Depends on**: 6
**Files:**
- Modify: `web/src/pages/Settings.svelte`

**What to do:**
- On mount, fetch `tlsApi.get()` and `tlsApi.publicIp()`.
- Display public IP in a copyable callout: "Point A record for api.example.com to {ip}".
- Form fields: Domain, Email, Enable HTTPS toggle, Accept Let's Encrypt ToS toggle, Staging toggle.
- Button "Check DNS" calls `tlsApi.checkDns(domain)` and shows resolved IPs + match status.
- Save action validates domain/email and calls `tlsApi.save`, then shows toast success.
- If config is enabled, show a warning banner: "Restart AxonRouter to activate HTTPS on port 443".

**Verify:**
- `cd web && npm run build` → zero warnings.

---

## Task Type

modification
