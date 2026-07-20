---
name: release-version
description: AxonRouter-Go release workflow — commit uncommitted changes, update CHANGELOG, bump to next patch version, run tests, tag, and push.
license: MIT
compatibility: opencode
metadata:
  project: AxonRouter-Go
---

## When to use

Use this skill when the user asks to "release", "update version", "bump version", or "buat release" for this repository.

## Workflow

### 1. Inspect current state

```bash
echo "=== Current version ==="
cat internal/version/VERSION

echo "=== Git status ==="
git status --short

echo "=== Recent commits ==="
git log --oneline -10
```

### 2. Commit any uncommitted changes

If `git status --short` shows modified files:

- Summarize the diff into a concise commit message.
- Add and commit with `git add <files> && git commit -m "..."`.
- If you see files you did **not** modify in this session, ask the user before committing.

### 3. Ensure CHANGELOG has Unreleased entries

Read the `## [Unreleased]` section of `CHANGELOG.md`.

- If it only contains empty categories, look at recent commits since the last tag and add a short human-readable entry under `### Added` / `Changed` / `Fixed` / `Removed` as appropriate.
- Do **not** make up entries if you cannot determine what changed. If in doubt, ask the user.

### 4. Compute the next patch version

```bash
current=$(cat internal/version/VERSION)
major=$(echo "$current" | cut -d. -f1)
minor=$(echo "$current" | cut -d. -f2)
patch=$(echo "$current" | cut -d. -f3)
next="${major}.${minor}.$((patch + 1))"
echo "Next version: $next (from $current)"
```

If the user explicitly asks for a different version (e.g. "release 0.4.0"), use the requested version instead.

### 5. Verify the build

Run these **in order**. Stop if any fail.

```bash
go build ./...
go test ./...
cd web && npm run build
cd web && npm run test
```

### 6. Run the release

```bash
make release "v=$next"
```

If `make release` fails at the push step or still references the wrong branch, fall back to manual push:

```bash
git push origin master
git push origin "v$next"
```

### 7. Verify the remote tag

```bash
git ls-remote --tags origin "v$next"
```

### 8. Report back

Tell the user:

- The version released.
- The release commit hash.
- Whether the working tree was dirty and, if so, that it was committed first.
- Any verification failures that need attention.

## Safety rules

- Do **not** release if `go build ./...`, `go test ./...`, or `npm run build` fail. Ask the user how to proceed instead.
- Do **not** commit files that were not modified in the current session without asking.
- Do **not** fabricate CHANGELOG entries. Add only what is supported by commit messages or recent diffs.
