# axonrouter-go

NPM installer for [AxonRouter-Go](https://github.com/rickicode/AxonRouter-Go).

This package downloads the correct pre-built AxonRouter-Go binary for your platform during `npm install` and verifies its SHA256 checksum.

## Install

```bash
npm install -g axonrouter-go
```

After installation you can run:

```bash
axonrouter --help
axonrouter --startup install-root
```

## How it works

- `postinstall` downloads `axonrouter-<os>-<arch>` (with `.exe` on Windows) from the matching GitHub Release.
- The binary is written to `node_modules/axonrouter-go/bin/`.
- On Linux/macOS, the script also tries to copy it to `~/.local/bin/axonrouter` so it is available on PATH.
- `axonrouter` / `axonrouter-go` CLI entries forward arguments to the downloaded binary.

## Environment variables

- `AXONROUTER_VERSION` — override the release version to download (default: the package version).
- `SKIP_AXONROUTER_DOWNLOAD=true` — skip the binary download during install.

## License

MIT
