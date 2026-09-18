# VAUI

## Overview

VAUI is a terminal UI for browsing and managing secrets in a HashiCorp Vault KV v2 mount. It can list paths and view, add, edit, or permanently delete secrets.

## How to Use This

Install the latest version directly from GitHub (Go 1.24 or newer is required):

```sh
go install github.com/stuft2/vaui/cmd/vaui@latest
```

Make sure the Go binary directory is on your `PATH`, then configure Vault and start VAUI:

```sh
export VAULT_ADDR=https://vault.example.com
vault login
vaui
```

VAUI uses `VAULT_TOKEN` when it is set; otherwise it reads the token saved by `vault login` in `~/.vault-token`. You can also pass `-token` explicitly. The default mount is `secret`. Override it with `-mount`, or set `VAULT_KV2_MOUNTS`, which is also used by VSH. When the variable contains multiple comma-separated mounts, VAUI uses the first one. Use `-namespace` or `VAULT_NAMESPACE` for Vault Enterprise. Run `vaui -help` for all options.

Inside the UI, use the arrow keys to navigate, Enter to open a path or secret, `/` to filter entries in the current path, `a` to add, `e` to edit, `d` to delete, and `q` to quit. Filtering is case-insensitive; press Enter to apply it or Escape to clear it. Secret values are masked by default; select a field and press `r` to reveal or hide it, or `c` to copy its value.

The structured editor supports adding, renaming, updating, and removing fields. Values use JSON syntax so their types remain explicit: for example, `"text"`, `42`, `true`, `null`, arrays, or objects. Press `j` from the structured editor to edit the entire secret as raw JSON. Save either editor with Ctrl+S.

The token needs `list` access to `<mount>/metadata/*`, read/write access to `<mount>/data/*`, and delete access to `<mount>/metadata/*` for the corresponding operations.

## How to Build Locally

Clone the repository and run the CLI from the project root:

```sh
git clone https://github.com/stuft2/vaui.git
cd vaui
export VAULT_ADDR=https://vault.example.com
vault login
go run ./cmd/vaui
```

Build a local binary with:

```sh
go build ./cmd/vaui
```

## How to Publish

The repository does not currently define an automated release workflow. Go users can install a tagged version with `go install github.com/stuft2/vaui/cmd/vaui@<version>` after a GitHub release is created.

## Architectural Overview

The `cmd/vaui` package parses configuration and connects the terminal UI in `internal/tui` to the HashiCorp Vault KV v2 HTTP client in `internal/vault`. The client sends requests directly to Vault and does not persist secrets locally.

## External Dependencies

- A reachable HashiCorp Vault server with a KV v2 mount
- A Vault token with permissions appropriate to the operations being used
- Bubble Tea, Bubbles, and Lip Gloss for the terminal interface

## How to Test

Run the unit tests with:

```sh
go test ./...
```
