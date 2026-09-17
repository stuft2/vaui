package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	addr := flag.String("addr", env("VAULT_ADDR", "http://127.0.0.1:8200"), "Vault server address")
	token := flag.String("token", os.Getenv("VAULT_TOKEN"), "Vault token (prefer VAULT_TOKEN)")
	mount := flag.String("mount", env("VAUI_MOUNT", "secret"), "KV v2 mount")
	namespace := flag.String("namespace", os.Getenv("VAULT_NAMESPACE"), "Vault Enterprise namespace")
	insecure := flag.Bool("insecure", false, "skip TLS certificate verification")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "VAULT_TOKEN or -token is required")
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

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
