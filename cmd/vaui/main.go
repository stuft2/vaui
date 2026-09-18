package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stuft2/vaui/internal/vault"
)

func main() {
	addr := flag.String("addr", env("VAULT_ADDR", "http://127.0.0.1:8200"), "Vault server address")
	token := flag.String("token", os.Getenv("VAULT_TOKEN"), "Vault token (defaults to VAULT_TOKEN or the Vault CLI login token)")
	mount := flag.String("mount", strings.Join(defaultMounts(), ","), "KV v2 mount(s), comma-separated")
	namespace := flag.String("namespace", os.Getenv("VAULT_NAMESPACE"), "Vault Enterprise namespace")
	insecure := flag.Bool("insecure", false, "skip TLS certificate verification")
	flag.Parse()

	var err error
	*token, err = resolveToken(*token)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, vault.ErrNotLoggedIn)
		os.Exit(2)
	}

	program, err := wire(config{
		address:   *addr,
		token:     *token,
		mounts:    parseMounts(*mount),
		namespace: *namespace,
		insecure:  *insecure,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolveToken(token string) (string, error) {
	if token != "" {
		return token, nil
	}
	return vaultLoginToken()
}

func vaultLoginToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find Vault login token: %w", err)
	}

	contents, err := os.ReadFile(filepath.Join(home, ".vault-token"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read Vault login token: %w", err)
	}

	return strings.TrimSpace(string(contents)), nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func defaultMounts() []string { return parseMounts(os.Getenv("VAULT_KV2_MOUNTS")) }
func parseMounts(value string) []string {
	seen := map[string]bool{}
	var mounts []string
	for _, raw := range strings.Split(value, ",") {
		mount := strings.Trim(strings.TrimSpace(raw), "/")
		if mount != "" && !seen[mount] {
			seen[mount] = true
			mounts = append(mounts, mount)
		}
	}
	return mounts
}
