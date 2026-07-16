# axonrouter-go

NPM installer for [AxonRouter-Go](https://github.com/rickicode/AxonRouter-Go).

This package downloads the correct pre-built AxonRouter-Go binary for your platform during `npm install` and verifies its SHA256 checksum.

## Install

```bash
npm install -g axonrouter-go
```

Or try it once without installing (requires the package to be published):

```bash
npx axonrouter-go --help
npx axonrouter-go
```

## How it works

- `postinstall` downloads `axonrouter-<os>-<arch>` (with `.exe` on Windows) from the matching GitHub Release.
- The binary is written to `node_modules/axonrouter-go/bin/`.
- On Linux/macOS, the script also tries to copy it to `~/.local/bin/axonrouter` so it is available on PATH after installation.
- `axonrouter` / `axonrouter-go` CLI entries forward arguments to the downloaded binary.

## Notes

- `npx` is great for one-off commands. For installing a systemd service, use `npm install -g axonrouter-go` or the shell installer so the binary path stays stable.

## Environment variables

- `AXONROUTER_VERSION` — override the release version to download (default: the package version).
- `SKIP_AXONROUTER_DOWNLOAD=true` — skip the binary download during install.

## License

MIT
