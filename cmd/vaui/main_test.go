package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenPrefersExplicitToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".vault-token"), []byte("login-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveToken("explicit-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "explicit-token" {
		t.Fatalf("resolveToken() = %q, want %q", got, "explicit-token")
	}
}

func TestVaultLoginToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".vault-token"), []byte(" logged-in-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := vaultLoginToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "logged-in-token" {
		t.Fatalf("vaultLoginToken() = %q, want %q", got, "logged-in-token")
	}
}

func TestVaultLoginTokenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := vaultLoginToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("vaultLoginToken() = %q, want empty token", got)
	}
}

func TestVaultLoginTokenReadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".vault-token"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := vaultLoginToken(); err == nil {
		t.Fatal("vaultLoginToken() error = nil, want read error")
	}
}

func TestDefaultMount(t *testing.T) {
	tests := []struct {
		name   string
		mounts string
		want   string
	}{
		{name: "default", want: "secret"},
		{name: "single mount", mounts: "kvv2", want: "kvv2"},
		{name: "first of multiple mounts", mounts: " kvv2, legacy ", want: "kvv2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VAULT_KV2_MOUNTS", tt.mounts)
			if got := defaultMount(); got != tt.want {
				t.Fatalf("defaultMount() = %q, want %q", got, tt.want)
			}
		})
	}
}
