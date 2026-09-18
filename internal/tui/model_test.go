package tui

import (
	"context"
	"fmt"
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

func TestPathListScrollsToKeepSelectionVisible(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.loading = false
	for i := range 20 {
		model.keys = append(model.keys, fmt.Sprintf("item-%02d", i))
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = updated.(Model)
	for range 10 {
		model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}

	view := model.View()
	if model.listOffset != 8 {
		t.Fatalf("list offset = %d, want 8", model.listOffset)
	}
	if strings.Contains(view, "item-00") || !strings.Contains(view, "> item-10") {
		t.Fatalf("scrolled view does not keep selection visible:\n%s", view)
	}
	if !strings.Contains(view, "11/20") {
		t.Fatalf("scrolled view does not show position:\n%s", view)
	}
	if lines := len(strings.Split(view, "\n")); lines > model.height {
		t.Fatalf("rendered %d lines in a %d-line terminal:\n%s", lines, model.height, view)
	}
}

func TestPathListResizeKeepsValidViewport(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.loading = false
	for i := range 10 {
		model.keys = append(model.keys, fmt.Sprintf("item-%02d", i))
	}
	model.cursor = 9
	model.listOffset = 7

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	model = updated.(Model)

	if model.cursor != 9 || model.listOffset != 3 {
		t.Fatalf("resized viewport = (cursor %d, offset %d), want (9, 3)", model.cursor, model.listOffset)
	}
	if !strings.Contains(model.View(), "> item-09") {
		t.Fatalf("resized view lost selection:\n%s", model.View())
	}
}

func TestFilteringResetsPathListViewport(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.keys = []string{"apps/", "argocd/", "token"}
	model.cursor = 2
	model.listOffset = 2
	model.mode = filtering
	model.filter.Focus()

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("app")})

	if model.cursor != 0 || model.listOffset != 0 {
		t.Fatalf("filtered viewport = (cursor %d, offset %d), want (0, 0)", model.cursor, model.listOffset)
	}
	if got := model.visibleKeys(); !reflect.DeepEqual(got, []string{"apps/"}) {
		t.Fatalf("visible keys = %#v, want only apps", got)
	}
}

func TestSecretValuesAreMaskedByDefault(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.mode = view
	model.selected = "apps/service"
	model.data = map[string]any{
		"enabled":  true,
		"password": "top-secret",
	}

	view := model.View()
	if strings.Contains(view, "top-secret") || strings.Contains(view, "true") {
		t.Fatalf("secret view exposed values by default:\n%s", view)
	}
	if !strings.Contains(view, "enabled: ••••••••") || !strings.Contains(view, "password: ••••••••") {
		t.Fatalf("secret view did not show masked fields:\n%s", view)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	view = model.View()
	if !strings.Contains(view, "enabled: true") || strings.Contains(view, "top-secret") {
		t.Fatalf("revealed view did not render only the selected non-string value:\n%s", view)
	}
}

func TestRevealAndHideSelectedSecretValue(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.mode = view
	model.data = map[string]any{"password": "top-secret", "username": "person"}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !strings.Contains(model.View(), `password: "top-secret"`) {
		t.Fatalf("revealed view did not show selected value:\n%s", model.View())
	}
	if strings.Contains(model.View(), `username: "person"`) {
		t.Fatalf("revealed view exposed an unselected value:\n%s", model.View())
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if strings.Contains(model.View(), "top-secret") {
		t.Fatalf("hidden view still exposed selected value:\n%s", model.View())
	}
}

func TestCopySelectedSecretValue(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.mode = view
	model.data = map[string]any{
		"config":   map[string]any{"enabled": true},
		"password": "top-secret",
	}
	var copied string
	model.copyValue = func(value string) error {
		copied = value
		return nil
	}

	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("copy returned no command")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)

	if copied != `{"enabled":true}` {
		t.Fatalf("copied value = %q, want compact JSON", copied)
	}
	if model.mode != view || model.status != "Copied value for config" {
		t.Fatalf("copy result = (mode %v, status %q)", model.mode, model.status)
	}
	if strings.Contains(model.status, copied) {
		t.Fatalf("copy status exposed copied value: %q", model.status)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model.copyValue = func(value string) error {
		copied = value
		return nil
	}
	_, cmd = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	cmd()
	if copied != "top-secret" {
		t.Fatalf("copied string = %q, want unquoted value", copied)
	}
}

func TestStructuredSecretEditorMutationsAndSave(t *testing.T) {
	store := &fakeSecretStore{}
	model := New(store)
	model.mode = view
	model.selected = "apps/service"
	model.data = map[string]any{"count": float64(2), "name": "original"}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if model.mode != editFields {
		t.Fatalf("edit mode = %v, want structured field editor", model.mode)
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model.fieldName.SetValue("total")
	model.fieldValue.SetValue("3")
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})

	if model.mode != editFields || model.draft["total"] != float64(3) {
		t.Fatalf("renamed field result = (mode %v, draft %#v)", model.mode, model.draft)
	}
	if _, exists := model.draft["count"]; exists {
		t.Fatalf("renamed draft retained old field: %#v", model.draft)
	}
	if model.data["count"] != float64(2) {
		t.Fatalf("structured draft mutated original data: %#v", model.data)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model.fieldName.SetValue("enabled")
	model.fieldValue.SetValue("true")
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.draft["enabled"] != true {
		t.Fatalf("added field = %#v, want boolean true", model.draft["enabled"])
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if _, exists := model.draft["enabled"]; exists {
		t.Fatalf("removed field remains in draft: %#v", model.draft)
	}

	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("structured save returned no command")
	}
	cmd()
	want := map[string]any{"name": "original", "total": float64(3)}
	if store.writtenName != "apps/service" || !reflect.DeepEqual(store.writtenData, want) {
		t.Fatalf("structured save = (%q, %#v), want (%q, %#v)", store.writtenName, store.writtenData, "apps/service", want)
	}
}

func TestStructuredFieldValidation(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.draft = map[string]any{"first": "one", "second": "two"}
	model.startFieldEdit("first", "one")

	model.fieldName.SetValue("second")
	model.fieldValue.SetValue(`"changed"`)
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.err == nil || !strings.Contains(model.err.Error(), "already exists") || model.mode != editField {
		t.Fatalf("duplicate validation = (mode %v, error %v)", model.mode, model.err)
	}

	model.fieldName.SetValue("renamed")
	model.fieldValue.SetValue(`{"nested":{"key":1,"key":2}}`)
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.err == nil || !strings.Contains(model.err.Error(), `duplicate field "key"`) || model.mode != editField {
		t.Fatalf("JSON validation = (mode %v, error %v)", model.mode, model.err)
	}

	model.fieldValue.SetValue("{")
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.err == nil || !strings.Contains(model.err.Error(), "invalid JSON value") {
		t.Fatalf("malformed JSON validation error = %v", model.err)
	}

	model.fieldName.SetValue("  ")
	model.fieldValue.SetValue("null")
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.err == nil || !strings.Contains(model.err.Error(), "cannot be empty") {
		t.Fatalf("empty-name validation error = %v", model.err)
	}
}

func TestRawJSONEditorPreservesAdvancedValues(t *testing.T) {
	store := &fakeSecretStore{}
	model := New(store)
	model.mode = view
	model.selected = "apps/service"
	model.data = map[string]any{"nested": map[string]any{"items": []any{"one", float64(2)}}}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.mode != edit {
		t.Fatalf("raw editor mode = %v, want edit", model.mode)
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.mode != editFields {
		t.Fatalf("raw editor escape mode = %v, want structured editor", model.mode)
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model.editor.SetValue(`{"enabled":true,"count":2,"nested":{"items":["one",2]}}`)
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("raw JSON save returned no command")
	}
	cmd()
	want := map[string]any{
		"enabled": true,
		"count":   float64(2),
		"nested":  map[string]any{"items": []any{"one", float64(2)}},
	}
	if !reflect.DeepEqual(store.writtenData, want) {
		t.Fatalf("raw JSON save = %#v, want %#v", store.writtenData, want)
	}
}

func TestRawJSONEditorRejectsDuplicateFields(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.mode = edit
	model.selected = "apps/service"
	model.editor.SetValue(`{"first":1,"first":2}`)

	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})

	if cmd != nil || model.err == nil || !strings.Contains(model.err.Error(), `duplicate field "first"`) {
		t.Fatalf("duplicate raw JSON result = (command %v, error %v)", cmd, model.err)
	}
}

func TestNewSecretStartsStructuredFieldEditor(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.mode = addName
	model.name.SetValue("apps/new-service")

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.mode != editField || model.selected != "apps/new-service" || model.originalField != "" {
		t.Fatalf("new secret editor = (mode %v, selected %q, original field %q)", model.mode, model.selected, model.originalField)
	}
	if model.fieldValue.Value() != `"value"` {
		t.Fatalf("new field value = %q, want JSON string placeholder", model.fieldValue.Value())
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
