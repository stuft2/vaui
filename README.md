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

VAUI uses `VAULT_TOKEN` when it is set; otherwise it reads the token saved by `vault login` in `~/.vault-token`. You can also pass `-token` explicitly.

Once VAUI opens:

1. Use the arrow keys to select a path or secret.
2. Press Enter to open it and Backspace to return to the parent path.
3. Select a secret field and press `r` to reveal its masked value.
4. Press `?` to see the controls relevant to the current screen.

See the [interface guide](docs/interface-guide.md) for screen-by-screen shortcuts and recipes for adding, editing, restoring, and deleting secrets.

### Mounts

When no mounts are configured, VAUI attempts to detect accessible KV v2 mounts through Vault's UI mount listing. Override this best-effort detection with `-mount` or set comma-separated mounts in `VAULT_KV2_MOUNTS`; VAUI prompts you to choose when more than one is available. If discovery is unavailable or finds no mounts, VAUI asks you to configure one explicitly. Use `-namespace` or `VAULT_NAMESPACE` for Vault Enterprise. Run `vaui -help` for all options.

### Permissions

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
