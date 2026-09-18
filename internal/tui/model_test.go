package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stuft2/vaui/internal/secret"
)

type fakeSecretStore struct {
	listedPrefix     string
	listKeys         []string
	listErr          error
	readName         string
	readVersion      int
	readValue        secret.Value
	metadata         secret.Metadata
	metadataErr      error
	restoredName     string
	restoredVersion  int
	writtenName      string
	writtenData      map[string]any
	deletedName      string
	undeletedName    string
	undeletedVersion int
	destroyedName    string
	destroyedVersion int
	undeleteErr      error
	destroyErr       error
	mounts           []string
	mount            string
}

func (f *fakeSecretStore) Mounts() []string {
	if len(f.mounts) == 0 {
		return []string{"secret"}
	}
	return f.mounts
}
func (f *fakeSecretStore) Mount() string {
	if f.mount == "" {
		return f.Mounts()[0]
	}
	return f.mount
}
func (f *fakeSecretStore) SelectMount(value string) error { f.mount = value; return nil }
func (f *fakeSecretStore) Address() string                { return "https://vault.example.edu" }
func (f *fakeSecretStore) Namespace() string              { return "team" }

func (f *fakeSecretStore) List(_ context.Context, prefix string) ([]string, error) {
	f.listedPrefix = prefix
	return f.listKeys, f.listErr
}

func (f *fakeSecretStore) Read(_ context.Context, name string, version int) (secret.Value, error) {
	f.readName = name
	f.readVersion = version
	return f.readValue, nil
}

func (f *fakeSecretStore) Metadata(_ context.Context, _ string) (secret.Metadata, error) {
	return f.metadata, f.metadataErr
}
func (f *fakeSecretStore) Restore(_ context.Context, name string, version int) error {
	f.restoredName, f.restoredVersion = name, version
	return nil
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
func (f *fakeSecretStore) Undelete(_ context.Context, name string, version int) error {
	f.undeletedName, f.undeletedVersion = name, version
	return f.undeleteErr
}
func (f *fakeSecretStore) Destroy(_ context.Context, name string, version int) error {
	f.destroyedName, f.destroyedVersion = name, version
	return f.destroyErr
}

func TestSecretCommands(t *testing.T) {
	t.Parallel()

	store := &fakeSecretStore{
		listKeys:  []string{"apps/", "token"},
		readValue: secret.Value{Data: map[string]any{"password": "secret"}, Version: 3},
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

	readResult, ok := model.loadSecret("team/token", 0)().(readMsg)
	if !ok {
		t.Fatalf("loadSecret result has type %T, want readMsg", model.loadSecret("team/token", 0)())
	}
	if store.readName != "team/token" || !reflect.DeepEqual(readResult.value, store.readValue) {
		t.Fatalf("loadSecret = (%q, %#v), want (%q, %#v)", store.readName, readResult.value, "team/token", store.readValue)
	}

	written := map[string]any{"password": "changed"}
	writeResult, ok := model.writeSecret("team/token", written)().(actionMsg)
	if !ok {
		t.Fatalf("writeSecret result has type %T, want actionMsg", model.writeSecret("team/token", written)())
	}
	if store.writtenName != "team/token" || !reflect.DeepEqual(store.writtenData, written) || writeResult.text != "Saved team/token" {
		t.Fatalf("writeSecret = (%q, %#v, %q)", store.writtenName, store.writtenData, writeResult.text)
	}

	deleteResult, ok := model.deleteSecret("team/token")().(versionActionMsg)
	if !ok {
		t.Fatalf("deleteSecret result has type %T, want versionActionMsg", model.deleteSecret("team/token")())
	}
	if store.deletedName != "team/token" || deleteResult.text != "Soft-deleted current version of team/token" {
		t.Fatalf("deleteSecret = (%q, %q)", store.deletedName, deleteResult.text)
	}
}

func TestVersionHistoryInspectAndRestore(t *testing.T) {
	created := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	store := &fakeSecretStore{metadata: secret.Metadata{CurrentVersion: 3, Versions: []secret.Version{{Version: 3, CreatedTime: created}, {Version: 2, CreatedTime: created}}}}
	model := New(store)
	model.selected = "team/token"
	model.data = map[string]any{"password": "current"}
	model.value = secret.Value{Data: model.data, Version: 3, CreatedTime: created}
	model.metadata = store.metadata
	model.mode = view

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if model.mode != history || !strings.Contains(model.View(), "v3") || !strings.Contains(model.View(), "current") {
		t.Fatalf("history view missing metadata:\n%s", model.View())
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("inspect returned no command")
	}
	if _, ok := cmd().(readMsg); !ok {
		t.Fatal("inspect command did not return readMsg")
	}
	if store.readVersion != 2 {
		t.Fatalf("read version = %d, want 2", store.readVersion)
	}

	model.mode = history
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if model.mode != confirmRestore {
		t.Fatalf("mode = %v, want confirmRestore", model.mode)
	}
	model, cmd = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("restore returned no command")
	}
	_ = cmd()
	if store.restoredName != "team/token" || store.restoredVersion != 2 {
		t.Fatalf("restore = (%q, %d)", store.restoredName, store.restoredVersion)
	}
}

func TestMetadataFailureDoesNotBreakSecretView(t *testing.T) {
	store := &fakeSecretStore{metadataErr: fmt.Errorf("Vault: permission denied")}
	model := New(store)
	value := secret.Value{Data: map[string]any{"password": "secret"}, Version: 4}
	updated, cmd := model.Update(readMsg{name: "team/token", value: value})
	model = updated.(Model)
	if model.mode != view || cmd == nil {
		t.Fatalf("read result = mode %v, command %v", model.mode, cmd)
	}
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	if model.mode != view || model.err != nil || model.historyErr == nil {
		t.Fatalf("metadata error broke view: mode=%v err=%v historyErr=%v", model.mode, model.err, model.historyErr)
	}
	model.mode = history
	if !strings.Contains(model.View(), "Version history unavailable: Vault: permission denied") {
		t.Fatalf("missing permission explanation:\n%s", model.View())
	}
}

func TestPriorVersionRequiresRestoreBeforeEditing(t *testing.T) {
	model := New(&fakeSecretStore{})
	model.mode = view
	model.value = secret.Value{Data: map[string]any{"password": "old"}, Version: 2}
	model.data = model.value.Data
	model.metadata.CurrentVersion = 3
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if model.mode != view || !strings.Contains(model.status, "restore") {
		t.Fatalf("prior version edit = mode %v, status %q", model.mode, model.status)
	}
}

func TestSoftDeleteConfirmationAndCancellation(t *testing.T) {
	store := &fakeSecretStore{}
	model := New(store)
	model.mode, model.selected = view, "team/token"

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if model.mode != confirmDelete || !strings.Contains(model.View(), "Soft-delete") || !strings.Contains(model.View(), "undeleted") {
		t.Fatalf("soft-delete confirmation missing recovery wording:\n%s", model.View())
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if model.mode != view || store.deletedName != "" {
		t.Fatalf("cancel = mode %v, deleted %q", model.mode, store.deletedName)
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	result := cmd().(versionActionMsg)
	if store.deletedName != "team/token" || result.text != "Soft-deleted current version of team/token" {
		t.Fatalf("soft delete = (%q, %q)", store.deletedName, result.text)
	}
	updated, refresh := model.Update(result)
	model = updated.(Model)
	if model.mode != history || refresh == nil {
		t.Fatalf("soft delete result = mode %v, refresh %v; want history refresh", model.mode, refresh)
	}
}

func TestHistoryUndeleteAndFailure(t *testing.T) {
	deletedAt := time.Now()
	store := &fakeSecretStore{undeleteErr: fmt.Errorf("Vault: permission denied")}
	model := New(store)
	model.mode, model.selected = history, "team/token"
	model.metadata = secret.Metadata{Versions: []secret.Version{{Version: 2, DeletionTime: &deletedAt}}}
	if !strings.Contains(model.View(), "soft-deleted") {
		t.Fatalf("history does not distinguish deleted version:\n%s", model.View())
	}
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	msg := cmd().(versionActionMsg)
	if store.undeletedName != "team/token" || store.undeletedVersion != 2 {
		t.Fatalf("undelete = (%q, %d)", store.undeletedName, store.undeletedVersion)
	}
	updated, _ := model.Update(msg)
	model = updated.(Model)
	if model.mode != history || model.err == nil || !strings.Contains(model.err.Error(), "permission denied") {
		t.Fatalf("undelete failure = mode %v, error %v", model.mode, model.err)
	}
}

func TestDestroyRequiresTypedConfirmation(t *testing.T) {
	store := &fakeSecretStore{}
	model := New(store)
	model.mode, model.selected = history, "team/token"
	model.metadata = secret.Metadata{Versions: []secret.Version{{Version: 2}}}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if model.mode != confirmDestroy || !strings.Contains(model.View(), "cannot be undone") {
		t.Fatalf("destroy confirmation is not explicit:\n%s", model.View())
	}
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || model.err == nil || store.destroyedName != "" {
		t.Fatalf("empty confirmation destroyed secret: command=%v error=%v name=%q", cmd, model.err, store.destroyedName)
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.mode != history {
		t.Fatalf("cancel mode = %v, want history", model.mode)
	}

	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("destroy v2")})
	model, cmd = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd().(versionActionMsg)
	if msg.err != nil || store.destroyedName != "team/token" || store.destroyedVersion != 2 {
		t.Fatalf("destroy = (%q, %d, %v)", store.destroyedName, store.destroyedVersion, msg.err)
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

func TestNormalizePathAndBreadcrumb(t *testing.T) {
	if got := normalizePath(" /apps//prod/../dev "); got != "apps/dev/" {
		t.Fatalf("normalizePath = %q", got)
	}
	if got := breadcrumb("apps/dev/"); got != "/ › apps › dev" {
		t.Fatalf("breadcrumb = %q", got)
	}
}

func TestRecentPathsAreUniqueAndBounded(t *testing.T) {
	model := New(&fakeSecretStore{})
	for i := 0; i < 12; i++ {
		model.visit(fmt.Sprintf("team/%02d", i))
	}
	model.visit("team/05")
	if len(model.recent) != 10 || model.recent[0] != "team/05/" {
		t.Fatalf("recent = %#v", model.recent)
	}
}

func TestDirectPathAndAncestorNavigation(t *testing.T) {
	model := New(&fakeSecretStore{})
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model.pathInput.SetValue("/apps/prod/")
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.prefix != "" || cmd == nil {
		t.Fatalf("direct path = %q, command %v", model.prefix, cmd)
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if model.prefix != "apps/prod/" {
		t.Fatalf("loaded direct path = %q", model.prefix)
	}
	model, cmd = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyBackspace})
	if model.prefix != "apps/prod/" || cmd == nil {
		t.Fatalf("ancestor = %q, command %v", model.prefix, cmd)
	}
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	if model.prefix != "apps/" {
		t.Fatalf("loaded ancestor = %q", model.prefix)
	}
}

func TestMultipleMountsRequireSelectionAndResetPathState(t *testing.T) {
	store := &fakeSecretStore{mounts: []string{"secret", "shared"}}
	model := New(store)
	model.prefix, model.recent = "apps/", []string{"apps/"}
	if model.mode != mountPicker || model.Init() != nil {
		t.Fatalf("initial mode = %v", model.mode)
	}
	model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if store.mount != "shared" || model.prefix != "" || len(model.recent) != 0 || cmd == nil {
		t.Fatalf("mount switch = mount %q prefix %q recent %#v", store.mount, model.prefix, model.recent)
	}
}

func TestContextualHelpPreservesModeAndState(t *testing.T) {
	tests := []struct {
		name string
		mode mode
		want string
	}{
		{"browse", browse, "go to path"}, {"secret", view, "reveal"}, {"editor", editFields, "raw JSON"}, {"history", history, "undelete"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := New(&fakeSecretStore{})
			model.mode, model.prefix, model.selected = tt.mode, "apps/", "apps/token"
			model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
			if model.mode != help || !strings.Contains(model.View(), tt.want) {
				t.Fatalf("help for %v:\n%s", tt.mode, model.View())
			}
			model, _ = updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
			if model.mode != tt.mode || model.prefix != "apps/" || model.selected != "apps/token" {
				t.Fatalf("dismiss changed state: %#v", model)
			}
		})
	}
}

func TestConnectionContextDoesNotShowToken(t *testing.T) {
	view := New(&fakeSecretStore{}).View()
	if !strings.Contains(view, "Vault: https://vault.example.edu • Namespace: team • Mount: secret") || strings.Contains(view, "token") {
		t.Fatalf("unsafe or missing context:\n%s", view)
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

	if model.filter.Value() != "" {
		t.Fatalf("filter = %q, want cleared after navigation", model.filter.Value())
	}
	if cmd == nil {
		t.Fatal("opening a filtered directory returned no load command")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if model.prefix != "argocd/" {
		t.Fatalf("prefix = %q, want %q", model.prefix, "argocd/")
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
	if model.listOffset != 9 {
		t.Fatalf("list offset = %d, want 9", model.listOffset)
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

func TestDeniedPathKeepsCurrentListingAndHeaderVisible(t *testing.T) {
	store := &fakeSecretStore{listKeys: []string{"restricted/", "token"}}
	model := New(store)
	model.loading = false
	model.keys = store.listKeys
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 44, Height: 10})
	model = updated.(Model)

	model, cmd := updateWithKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	store.listErr = fmt.Errorf("Vault permission denied: request access for this mount and path (permission denied)")
	result := cmd().(listMsg)
	updated, _ = model.Update(result)
	model = updated.(Model)

	if model.prefix != "" {
		t.Fatalf("denied navigation changed path to %q", model.prefix)
	}
	view := model.View()
	if !strings.HasPrefix(view, titleStyle.Render("VAUI — Vault secrets")+"\n") {
		t.Fatalf("denied navigation lost header:\n%s", view)
	}
	if rows := terminalRows(view, model.width); rows > model.height {
		t.Fatalf("denied navigation rendered %d rows in a %d-row terminal:\n%s", rows, model.height, view)
	}
}

func terminalRows(view string, width int) int {
	rows := 0
	for _, line := range strings.Split(view, "\n") {
		rows += max(1, (lipgloss.Width(line)+width-1)/width)
	}
	return rows
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

	if model.cursor != 9 || model.listOffset != 4 {
		t.Fatalf("resized viewport = (cursor %d, offset %d), want (9, 4)", model.cursor, model.listOffset)
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
