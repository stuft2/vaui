package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeSecretStore struct {
	listedPrefix string
	listKeys     []string
	readName     string
	readData     map[string]any
	writtenName  string
	writtenData  map[string]any
	deletedName  string
}

func (f *fakeSecretStore) List(_ context.Context, prefix string) ([]string, error) {
	f.listedPrefix = prefix
	return f.listKeys, nil
}

func (f *fakeSecretStore) Read(_ context.Context, name string) (map[string]any, error) {
	f.readName = name
	return f.readData, nil
}

func (f *fakeSecretStore) Write(_ context.Context, name string, data map[string]any) error {
	f.writtenName = name
	f.writtenData = data
	return nil
}

func (f *fakeSecretStore) Delete(_ context.Context, name string) error {
	f.deletedName = name
	return nil
}

func TestSecretCommands(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{
		listKeys: []string{"apps/", "token"},
		readData: map[string]any{"password": "secret"},
	}
	model := New(store)
	model.prefix = "team/"

	listResult, ok := model.loadList()().(listMsg)
	if !ok {
		t.Fatalf("loadList result has type %T, want listMsg", model.loadList()())
	}
	if store.listedPrefix != "team/" || !reflect.DeepEqual(listResult.keys, store.listKeys) {
		t.Fatalf("loadList = (%q, %#v), want (%q, %#v)", store.listedPrefix, listResult.keys, "team/", store.listKeys)
	}

	readResult, ok := model.loadSecret("team/token")().(readMsg)
	if !ok {
		t.Fatalf("loadSecret result has type %T, want readMsg", model.loadSecret("team/token")())
	}
	if store.readName != "team/token" || !reflect.DeepEqual(readResult.data, store.readData) {
		t.Fatalf("loadSecret = (%q, %#v), want (%q, %#v)", store.readName, readResult.data, "team/token", store.readData)
	}

	written := map[string]any{"password": "changed"}
	writeResult, ok := model.writeSecret("team/token", written)().(actionMsg)
	if !ok {
		t.Fatalf("writeSecret result has type %T, want actionMsg", model.writeSecret("team/token", written)())
	}
	if store.writtenName != "team/token" || !reflect.DeepEqual(store.writtenData, written) || writeResult.text != "Saved team/token" {
		t.Fatalf("writeSecret = (%q, %#v, %q)", store.writtenName, store.writtenData, writeResult.text)
	}

	deleteResult, ok := model.deleteSecret("team/token")().(actionMsg)
	if !ok {
		t.Fatalf("deleteSecret result has type %T, want actionMsg", model.deleteSecret("team/token")())
	}
	if store.deletedName != "team/token" || deleteResult.text != "Deleted team/token" {
		t.Fatalf("deleteSecret = (%q, %q)", store.deletedName, deleteResult.text)
	}
}

func TestParent(t *testing.T) {
	tests := map[string]string{"": "", "apps/": "", "apps/prod/": "apps/", "apps/prod/db/": "apps/prod/"}
	for input, want := range tests {
		if got := parent(input); got != want {
			t.Errorf("parent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFilterPaths(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.keys = []string{"apps/", "argocd/", "token"}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("APP")})

	if model.mode != filtering {
		t.Fatalf("mode = %v, want filtering", model.mode)
	}
	view := model.View()
	if !strings.Contains(view, "apps/") || strings.Contains(view, "argocd/") || strings.Contains(view, "token") {
		t.Fatalf("filtered view contains unexpected paths:\n%s", view)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.mode != browse || model.filter.Value() != "APP" {
		t.Fatalf("applied filter = (%v, %q), want (browse, %q)", model.mode, model.filter.Value(), "APP")
	}
}

func TestFilterNavigationUsesVisiblePaths(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.keys = []string{"apps/", "argocd/", "token"}
	model.filter.SetValue("a")

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.prefix != "argocd/" {
		t.Fatalf("prefix = %q, want %q", model.prefix, "argocd/")
	}
	if model.filter.Value() != "" {
		t.Fatalf("filter = %q, want cleared after navigation", model.filter.Value())
	}
	if cmd == nil {
		t.Fatal("opening a filtered directory returned no load command")
	}
}

func TestEscapeClearsFilter(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.keys = []string{"apps/", "token"}
	model.mode = filtering
	model.filter.SetValue("apps")
	model.filter.Focus()

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if model.mode != browse || model.filter.Value() != "" || len(model.visibleKeys()) != 2 {
		t.Fatalf("cleared filter = (%v, %q, %v)", model.mode, model.filter.Value(), model.visibleKeys())
	}
}

func updateWithKey(t *testing.T, model Model, key tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := model.handleKey(key)
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("updated model has type %T, want Model", updated)
	}
	return result, cmd
}
