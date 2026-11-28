---
title: Install
description: Install poo via the release script, go install, or build from source with mise, and verify with poo version.
---

# Install

`poo` ships as a single static binary for Linux and macOS (`amd64` and `arm64`).

## Release script (recommended)

Download the latest GitHub release binary, verify its SHA-256 checksum against `checksums.txt`, and install it to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/sdougbrown/poo/main/install.sh | sh
```

To install into a different directory, set `BINDIR`:

```bash
curl -fsSL https://raw.githubusercontent.com/sdougbrown/poo/main/install.sh | BINDIR=/usr/local/bin sh
```

If `BINDIR` is not on your `PATH`, the script prints the `export PATH=...` line to add to your shell profile.

## `go install`

Requires **Go 1.26 or newer**:

```bash
go install github.com/sdougbrown/poo/cmd/poo@latest
```

Pin a specific release tag by replacing `@latest` with `@vX.Y.Z`.

## Build from source

Development uses [mise](https://mise.jdx.dev/) to pin Go 1.26 (see `mise.toml`):

```bash
git clone https://github.com/sdougbrown/poo.git
cd poo
mise install
mise exec -- go build -o poo ./cmd/poo
```

Run the vet and race-detector test suite to verify your checkout:

```bash
mise exec -- go vet ./...
mise exec -- go test -race ./...
```

## Verify the installation

`poo version` reports build information embedded in the binary. For a `go install ...@version` or release build, it shows the module version. For a checkout build, it shows the VCS revision, commit timestamp, and `modified` flag:

```bash
poo version
```

For scripts or CI pipelines that need to pin or inspect the binary, pass `--json`:

```bash
poo version --json
```
