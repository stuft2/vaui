package main

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/stuft2/vaui/internal/tui"
	"github.com/stuft2/vaui/internal/vault"
)

type config struct {
	address   string
	token     string
	mount     string
	namespace string
	insecure  bool
}

var _ tui.SecretStore = (*vault.Client)(nil)

// wire is the application's frameworkless composition root. It constructs and
// connects concrete dependencies while the consuming packages own their
// boundary interfaces.
func wire(cfg config) (*tea.Program, error) {
	client, err := vault.New(cfg.address, cfg.token, cfg.namespace, cfg.mount, cfg.insecure)
	if err != nil {
		return nil, err
	}

	return tea.NewProgram(tui.New(client), tea.WithAltScreen()), nil
}
