package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	addr := flag.String("addr", env("VAULT_ADDR", "http://127.0.0.1:8200"), "Vault server address")
	token := flag.String("token", os.Getenv("VAULT_TOKEN"), "Vault token (defaults to VAULT_TOKEN or the Vault CLI login token)")
	mount := flag.String("mount", defaultMount(), "KV v2 mount")
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
		fmt.Fprintln(os.Stderr, "Vault token not found; run vault login, set VAULT_TOKEN, or use -token")
		os.Exit(2)
	}

	program, err := wire(config{
		address:   *addr,
		token:     *token,
		mount:     *mount,
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

func defaultMount() string {
	mounts := strings.Split(env("VAULT_KV2_MOUNTS", "secret"), ",")
	return strings.TrimSpace(mounts[0])
}
