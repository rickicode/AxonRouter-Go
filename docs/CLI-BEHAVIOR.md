# AxonRouter-Go — CLI & Service Behavior Spec

## Behavior Summary

Program `axonrouter` punya 2 mode:
1. **Interactive Menu** (default) — tampilkan status, manage service
2. **Direct Run** (parameter) — langsung start server + stream logs

---

## 1. Interactive Menu (Default)

Ketika user jalankan `axonrouter` tanpa parameter:

```
╔══════════════════════════════════════════╗
║           AxonRouter-Go v1.0.0           ║
╠══════════════════════════════════════════╣
║                                          ║
║  Service Status: ● RUNNING               ║
║  Port: 3777                              ║
║  Uptime: 2h 15m                          ║
║  Requests: 1,234                         ║
║  PID: 12345                              ║
║                                          ║
║  1. Open Dashboard (browser)             ║
║  2. View Live Logs                       ║
║  3. Restart Service                      ║
║  4. Stop Service                         ║
║  5. Service Settings                     ║
║  6. Uninstall Service                    ║
║  7. Exit                                 ║
║                                          ║
╚══════════════════════════════════════════╝
```

### Service States:

**State: NOT INSTALLED**
```
╔══════════════════════════════════════════╗
║           AxonRouter-Go v1.0.0           ║
╠══════════════════════════════════════════╣
║                                          ║
║  Service Status: ○ NOT INSTALLED         ║
║                                          ║
║  1. Install Service                      ║
║  2. Run Directly (foreground)            ║
║  3. Exit                                 ║
║                                          ║
╚══════════════════════════════════════════╝
```

**State: INSTALLED + STOPPED**
```
╔══════════════════════════════════════════╗
║           AxonRouter-Go v1.0.0           ║
╠══════════════════════════════════════════╣
║                                          ║
║  Service Status: ◉ STOPPED               ║
║  Port: 3777                              ║
║  Last Run: 2h ago                        ║
║                                          ║
║  1. Start Service                        ║
║  2. Run Directly (foreground)            ║
║  3. Uninstall Service                    ║
║  4. Exit                                 ║
║                                          ║
╚══════════════════════════════════════════╝
```

**State: INSTALLED + RUNNING**
```
╔══════════════════════════════════════════╗
║           AxonRouter-Go v1.0.0           ║
╠══════════════════════════════════════════╣
║                                          ║
║  Service Status: ● RUNNING               ║
║  Port: 3777                              ║
║  Uptime: 2h 15m                          ║
║  Requests: 1,234 | Success: 98.5%        ║
║  PID: 12345                              ║
║                                          ║
║  1. Open Dashboard (browser)             ║
║  2. View Live Logs (attach to service)   ║
║  3. Restart Service                      ║
║  4. Stop Service                         ║
║  5. Service Settings                     ║
║  6. Uninstall Service                    ║
║  7. Exit                                 ║
║                                          ║
╚══════════════════════════════════════════╝
```

### Menu Actions:

| Action | Behavior |
|--------|----------|
| **Install Service** | Install sebagai system service (systemd/launchd/Windows service) |
| **Start Service** | Start service yang sudah installed |
| **Stop Service** | Stop service yang running |
| **Restart Service** | Stop + Start |
| **Uninstall Service** | Remove system service (data tetap) |
| **Open Dashboard** | Buka browser ke `http://localhost:3777` |
| **View Live Logs** | Attach ke running service, stream HTTP request logs real-time |
| **Run Directly** | Jalankan server di foreground (bukan service) |
| **Exit** | Keluar dari program |

### View Live Logs (Attach Mode)

Ketika service running, "View Live Logs" attach ke service dan stream logs:

```
[12:34:56] POST /v1/chat/completions | openai/gpt-4o | 200 | 1.2s | 150 tokens
[12:34:57] POST /v1/messages | claude/claude-sonnet-4 | 200 | 2.1s | 300 tokens
[12:34:58] POST /v1/chat/completions | codex/gpt-5 | 502 | 0.5s | fallback → mimo/mimo-v2-pro
[12:34:59] POST /v1/audio/speech | openai/tts-1 | 200 | 0.8s | audio/mpeg
[12:35:00] POST /v1/images/generations | openai/dall-e-3 | 200 | 3.2s | 1024x1024
```

---

## 2. Direct Run Mode

### `axonrouter run`

Langsung start server di foreground, **tanpa menu interaktif**.

Behavior:
1. Cek apakah ada service yang running di port yang sama
2. Kalau ada → auto kill process tersebut
3. Start server di foreground
4. Stream HTTP request logs ke stdout
5. Ctrl+C untuk stop

```
$ axonrouter run
Killing existing process on port 3777 (PID: 12345)...
Starting AxonRouter-Go on :3777...
Dashboard: http://localhost:3777

[12:34:56] POST /v1/chat/completions | openai/gpt-4o | 200 | 1.2s
[12:34:57] POST /v1/messages | claude/claude-sonnet-4 | 200 | 2.1s
^C
Shutting down...
```

### `axonrouter run --port 8080`

Custom port.

### `axonrouter run --no-kill`

Jangan auto kill, error kalau port sudah dipakai.

---

## 3. Other CLI Commands

```
axonrouter                    # Interactive menu (default)
axonrouter run                # Direct run (foreground, auto-kill, stream logs)
axonrouter run --port 8080    # Direct run on custom port
axonrouter run --no-kill      # Direct run, fail if port in use
axonrouter status             # Show service status (non-interactive)
axonrouter stop               # Stop service
axonrouter restart            # Restart service
axonrouter version            # Show version
axonrouter help               # Show help
```

---

## 4. Implementation Notes

### Service Management

**Linux:** systemd user service (`~/.config/systemd/user/axonrouter.service`)
**macOS:** launchd plist (`~/Library/LaunchAgents/com.axonrouter.service.plist`)
**Windows:** Windows Service via `sc.exe`

### Port Conflict Detection

```go
func killProcessOnPort(port int) error {
    // Find process using port
    // Send SIGTERM
    // Wait 5s
    // If still running, SIGKILL
}

func isPortInUse(port int) bool {
    ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        return true
    }
    ln.Close()
    return false
}
```

### Log Streaming (Attach Mode)

Service menulis logs ke shared log file atau Unix socket.
CLI attach ke socket/file dan stream ke stdout.

Alternative: Service expose `/api/logs/stream` endpoint (SSE), CLI connect ke sana.

### Process Detection

```go
func findServiceProcess() (pid int, running bool) {
    // Check PID file: ~/.axonrouter/axonrouter.pid
    // Verify process still running
    // Return PID and status
}
```

---

## 5. State Diagram

```
axonrouter (no args)
    │
    ├─ Check: service installed?
    │   ├─ No → Show: [Install] [Run Directly] [Exit]
    │   └─ Yes → Check: service running?
    │       ├─ No → Show: [Start] [Run Directly] [Uninstall] [Exit]
    │       └─ Yes → Show: [Dashboard] [Logs] [Restart] [Stop] [Settings] [Uninstall] [Exit]
    │
    └─ User selects action → execute

axonrouter run
    │
    ├─ Check: port in use?
    │   ├─ Yes + --no-kill → Error: port in use
    │   ├─ Yes → Kill process → Start server
    │   └─ No → Start server
    │
    └─ Stream logs to stdout until Ctrl+C
```
