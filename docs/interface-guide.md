# VAUI interface guide

Press `?` from any screen to see its controls. This guide groups those shortcuts by task so you can find an action without stepping through the interface.

## Find and open a secret

The browse screen starts at the root of the selected Vault mount. Entries ending in `/` are paths; other entries are secrets.

| Key | Action |
| --- | --- |
| `↑` / `↓` or `k` / `j` | Move through the current list |
| Enter, `l`, or `v` | Open the selected path or secret |
| Backspace or `h` | Go to the parent path |
| `/` | Filter entries in the current path |
| `g` | Enter a path directly |
| `p` | Choose a recently visited path |
| `m` | Switch mounts when multiple mounts are configured |
| `a` | Add a secret |
| `q` | Quit VAUI |

Filtering is case-insensitive. Press Enter to keep the filter while browsing, or Escape to clear it. Opening a path also clears the filter.

## View a secret safely

Secret values are masked when you open a secret. Move to a field before revealing or copying it.

| Key | Action |
| --- | --- |
| `↑` / `↓` or `k` / `j` | Select a field |
| `r` | Reveal or mask the selected value |
| `c` | Copy the selected value |
| `e` | Edit the current version |
| `h` | Open version history |
| `d` | Soft-delete the current version |
| Escape or `q` | Return to browsing |

## Add or edit a secret

To add a secret:

1. Press `a` from the browse screen.
2. Enter the full secret path and press Enter.
3. Enter a field name and its JSON value, using Tab to switch between them.
4. Press Ctrl+S to apply the field, then add or edit any other fields.
5. Press Ctrl+S from the field list to save the secret to Vault.

To edit an existing secret, open it and press `e`. The structured editor provides these actions:

| Key | Action |
| --- | --- |
| `↑` / `↓` | Select a field |
| Enter | Edit the selected field |
| `a` | Add a field |
| `d` | Remove the selected field from the draft |
| `j` | Edit the entire secret as raw JSON |
| Ctrl+S | Save the secret to Vault |
| Escape | Cancel and return to the secret |

Field values use JSON syntax so Vault data types remain explicit:

```json
{
  "username": "service-account",
  "retries": 3,
  "enabled": true,
  "labels": ["production", "api"]
}
```

Strings require quotes. Numbers, booleans, `null`, arrays, and objects use normal JSON syntax. In the raw JSON editor, the top-level value must be an object.

## Work with version history

Open a secret and press `h` to see its versions and whether each one is current, active, soft-deleted, or permanently destroyed.

| Key | Action |
| --- | --- |
| `↑` / `↓` or `k` / `j` | Select a version |
| Enter | Inspect an active version |
| `r` | Restore an active version as a new current version |
| `u` | Undelete a soft-deleted version |
| `x` | Permanently destroy a version |
| Escape or `q` | Return to the secret |

You cannot edit an older version directly. Restore it first, which writes its data as a new current version.

## Understand deletion

The two deletion actions have different consequences:

- Pressing `d` on a secret **soft-deletes its current version**. You can recover that version from history with `u`.
- Pressing `x` on a version in history **permanently destroys that version**. VAUI requires you to type the displayed confirmation text exactly, and the action cannot be undone.

## Leave or cancel a screen

Press `?` to open or close contextual help. Escape returns to the previous screen or cancels the current prompt in most places. Ctrl+C exits VAUI from any screen.
