---
type: note
tags:
- multica
- architecture
- workflow
permalink: axonrouter-go/multica-architecture
---

# Multica Architecture & Workflow

## 3 Komponen

1. **Multica Server** (multica.ai) — database, issue tracker, WebSocket hub. TIDAK menjalankan kode.
2. **Daemon** (local, `multica daemon start`) — detect AI tools, poll task setiap 3 detik, heartbeat 15 detik.
3. **AI Coding Tool** (OpenCode, Claude Code, Codex, dll) — yang benar-benar nulis kode. Spawn per task.

## Lifecycle Task

1. Assign issue ke agent → server buat task `queued`
2. Daemon detect (≤3 detik) → task `dispatched`
3. Daemon buat isolated worktree per task → spawn AI tool → task `running`
4. AI nulis kode, test, post comment → task `completed` / `failed`

## Worktree Model

```
~/.multica_workspaces/
├── .repos/
│   └── <workspace-id>/
│       └── github.com+owner+repo.git  ← bare clone (shared)
│
└── <workspace-id>/
    ├── <task-id-1>/workdir/<repo>/  ← worktree branch agent/<name>/<task-id>
    ├── <task-id-2>/workdir/<repo>/  ← worktree branch HIJ-<num>-<desc>
    └── ...
```

- Setiap task = 1 git worktree di branch terpisah
- Banyak agent bisa parallel tanpa conflict
- Worktree di-cleanup otomatis kalau task selesai (done/cancelled)
- Bisa buka worktree langsung di editor: `~/.multica_workspaces/<workspace-id>/<task-id>/workdir/<repo>/`

## 4 Cara Trigger Agent

| Cara | Command |
|---|---|
| Assign issue | `multica issue assign HIJ-<num> --to "Agent Name"` |
| @mention di comment | Tulis `@Agent Name` di comment body |
| Direct chat | Chat langsung ke agent (bukan issue) |
| Autopilot/webhook | PR opened → auto trigger agent |

## CLI Critical Patterns

- Comment multi-line: WAJIB `--content-file`, jangan `--content` inline
- Comment reply: WAJIB `--parent <trigger-comment-id>`
- Status: `multica issue status <id> <status>` (bukan `multica issue update --status`)
- Assign: `multica issue assign <id> --to "Name"` (bukan `--assignee`)
- Tidak ada `issue delete`: pakai `multica issue status <id> cancelled`
- Avatar: `multica agent avatar <id> --file <png>` (bukan `--avatar-url`)
- Agent instructions panjang: simpan ke file, pakai `$(cat file)` di shell

## Concurrency

- Agent default: max 6 concurrent tasks (`max_concurrent_tasks`)
- Daemon default: max 20 concurrent tasks
- Limit terkecil yang menang

## Garbage Collection

- Task `done`/`cancelled` idle > TTL → worktree dihapus
- Orphan dirs tanpa `.gc_meta.json` → dihapus
- Artifact-only cleanup: `node_modules`, `.next`, `.turbo` dihapus, source dipertahankan

## Skill Reference

Multica CLI cheat sheet skill: `/home/agent/.pi/agent/skills/multica-cli/SKILL.md`