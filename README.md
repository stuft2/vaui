# VAUI

VAUI is a terminal UI for browsing and managing secrets in a HashiCorp Vault KV v2 mount. It can list paths and view, add, edit, or permanently delete secrets.

## Run

```sh
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=…
go run ./cmd/vaui
```

The default mount is `secret`. Override it with `-mount`, and use `-namespace` for Vault Enterprise. Run `go run ./cmd/vaui -help` for all options.

Inside the UI, use the arrow keys to navigate, Enter to open a path or secret, `a` to add, `e` to edit, `d` to delete, and `q` to quit. Secret values are edited as JSON; save with Ctrl+S.

The token needs `list` access to `<mount>/metadata/*`, read/write access to `<mount>/data/*`, and delete access to `<mount>/metadata/*` for the corresponding operations.
