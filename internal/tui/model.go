package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stuft2/vaui/internal/secret"
)

// SecretStore describes the Vault operations the terminal UI consumes.
type SecretStore interface {
	List(context.Context, string) ([]string, error)
	Read(context.Context, string, int) (secret.Value, error)
	Metadata(context.Context, string) (secret.Metadata, error)
	Restore(context.Context, string, int) error
	Write(context.Context, string, map[string]any) error
	Delete(context.Context, string) error
	Undelete(context.Context, string, int) error
	Destroy(context.Context, string, int) error
	Mounts() []string
	Mount() string
	SelectMount(string) error
	Address() string
	Namespace() string
}

type mode int

const (
	browse mode = iota
	filtering
	view
	editFields
	editField
	edit
	addName
	confirmDelete
	history
	confirmRestore
	confirmDestroy
	directPath
	recentPaths
	mountPicker
	help
)

type listMsg struct {
	prefix string
	keys   []string
	err    error
}
type readMsg struct {
	name    string
	value   secret.Value
	version int
	err     error
}
type metadataMsg struct {
	metadata secret.Metadata
	err      error
}
type restoreMsg struct {
	version int
	err     error
}
type versionActionMsg struct {
	text string
	err  error
}
type actionMsg struct {
	text string
	err  error
}
type copyMsg struct {
	key string
	err error
}

type Model struct {
	secrets        SecretStore
	mode           mode
	prefix         string
	keys           []string
	cursor         int
	listOffset     int
	selected       string
	data           map[string]any
	value          secret.Value
	metadata       secret.Metadata
	historyErr     error
	historyCursor  int
	draft          map[string]any
	fieldCursor    int
	revealed       map[string]bool
	originalField  string
	fieldName      textinput.Model
	fieldValue     textarea.Model
	fieldNameFocus bool
	status         string
	err            error
	loading        bool
	width, height  int
	editor         textarea.Model
	name           textinput.Model
	filter         textinput.Model
	destroyConfirm textinput.Model
	copyValue      func(string) error
	pathInput      textinput.Model
	pickerCursor   int
	recent         []string
	helpReturn     mode
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func New(secrets SecretStore) Model {
	ed := textarea.New()
	ed.SetWidth(72)
	ed.SetHeight(16)
	ed.ShowLineNumbers = true
	name := textinput.New()
	name.Placeholder = "path/to/secret"
	name.CharLimit = 512
	name.Width = 60
	filter := textinput.New()
	filter.Placeholder = "filter current path"
	filter.CharLimit = 256
	filter.Width = 60
	fieldName := textinput.New()
	fieldName.Placeholder = "field name"
	fieldName.CharLimit = 512
	fieldName.Width = 60
	fieldValue := textarea.New()
	fieldValue.Placeholder = `JSON value, for example "secret", 42, true, or {"key":"value"}`
	fieldValue.SetWidth(72)
	fieldValue.SetHeight(8)
	fieldValue.ShowLineNumbers = false
	destroyConfirm := textinput.New()
	destroyConfirm.Width = 60
	pathInput := textinput.New()
	pathInput.Placeholder = "path/to/secrets"
	pathInput.Width = 60
	model := Model{
		secrets: secrets, loading: true, editor: ed, name: name, filter: filter,
		fieldName: fieldName, fieldValue: fieldValue, destroyConfirm: destroyConfirm, pathInput: pathInput,
		revealed: make(map[string]bool), copyValue: clipboard.WriteAll,
	}
	if len(secrets.Mounts()) > 1 {
		model.mode, model.loading = mountPicker, false
	}
	return model
}

func (m Model) Init() tea.Cmd {
	if m.mode == mountPicker {
		return nil
	}
	return m.loadList()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.editor.SetWidth(max(30, min(100, msg.Width-4)))
		m.editor.SetHeight(max(6, msg.Height-9))
		m.fieldValue.SetWidth(max(30, min(100, msg.Width-4)))
		m.fieldValue.SetHeight(max(4, msg.Height-12))
		m.ensureCursorVisible()
	case listMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.prefix = msg.prefix
			m.keys = msg.keys
			m.visit(m.prefix)
			visible := m.visibleKeys()
			if m.cursor >= len(visible) {
				m.cursor = max(0, len(visible)-1)
			}
			m.ensureCursorVisible()
		}
	case readMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.selected, m.value, m.data, m.mode = msg.name, msg.value, msg.value.Data, view
			m.historyErr = nil
			if msg.version == 0 {
				m.metadata = secret.Metadata{}
			}
			m.fieldCursor = 0
			m.revealed = make(map[string]bool)
			return m, m.loadMetadata(msg.name)
		}
	case metadataMsg:
		m.loading = false
		m.historyErr = msg.err
		if msg.err == nil {
			m.metadata = msg.metadata
			m.historyCursor = 0
		}
	case restoreMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = fmt.Sprintf("Restored version %d as a new version", msg.version)
			m.loading = true
			return m, m.loadSecret(m.selected, 0)
		}
	case versionActionMsg:
		m.loading = false
		m.err = msg.err
		m.mode = history
		if msg.err == nil {
			m.status = msg.text
			m.loading = true
			return m, m.loadMetadata(m.selected)
		}
	case actionMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.text
			m.mode = browse
			return m, m.loadList()
		}
	case copyMsg:
		m.err = msg.err
		if msg.err == nil {
			m.status = "Copied value for " + msg.key
		}
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if key.String() == "?" && m.mode != help {
		m.helpReturn, m.mode = m.mode, help
		return m, nil
	}
	if m.mode == help {
		if key.String() == "?" || key.String() == "esc" {
			m.mode = m.helpReturn
		}
		return m, nil
	}
	m.err = nil
	m.status = ""
	switch m.mode {
	case browse:
		visible := m.visibleKeys()
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "/":
			m.mode = filtering
			m.filter.Focus()
			m.ensureCursorVisible()
			return m, textinput.Blink
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.ensureCursorVisible()
		case "down", "j":
			if m.cursor+1 < len(visible) {
				m.cursor++
			}
			m.ensureCursorVisible()
		case "backspace", "h":
			if m.prefix != "" {
				m.clearFilter()
				m.cursor = 0
				m.loading = true
				return m, m.loadListAt(parent(m.prefix))
			}
		case "enter", "l", "v":
			if len(visible) == 0 {
				break
			}
			item := visible[m.cursor]
			if strings.HasSuffix(item, "/") {
				m.clearFilter()
				m.cursor = 0
				m.loading = true
				return m, m.loadListAt(m.prefix + item)
			}
			m.loading = true
			return m, m.loadSecret(m.prefix+item, 0)
		case "a":
			m.mode = addName
			m.name.SetValue(strings.TrimSuffix(m.prefix, "/"))
			m.name.CursorEnd()
			m.name.Focus()
			return m, textinput.Blink
		case "g":
			m.pathInput.SetValue(strings.TrimSuffix(m.prefix, "/"))
			m.pathInput.Focus()
			m.mode = directPath
			return m, textinput.Blink
		case "p":
			if len(m.recent) > 0 {
				m.pickerCursor = 0
				m.mode = recentPaths
			}
		case "m":
			if len(m.secrets.Mounts()) > 1 {
				m.pickerCursor = 0
				m.mode = mountPicker
			}
		}
	case filtering:
		switch key.String() {
		case "esc":
			m.clearFilter()
			m.mode = browse
			return m, nil
		case "enter":
			m.filter.Blur()
			m.mode = browse
			m.ensureCursorVisible()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(key)
		m.cursor = 0
		m.ensureCursorVisible()
		return m, cmd
	case view:
		fields := m.secretKeys()
		switch key.String() {
		case "esc", "q":
			m.mode = browse
		case "up", "k":
			if m.fieldCursor > 0 {
				m.fieldCursor--
			}
		case "down", "j":
			if m.fieldCursor+1 < len(fields) {
				m.fieldCursor++
			}
		case "r":
			if len(fields) > 0 {
				field := fields[m.fieldCursor]
				m.revealed[field] = !m.revealed[field]
			}
		case "c":
			if len(fields) > 0 {
				return m, m.copySelectedValue(fields[m.fieldCursor])
			}
		case "e":
			if m.viewingPriorVersion() {
				m.status = "Use history restore to make this version current before editing"
			} else {
				m.startStructuredEdit()
			}
		case "h":
			m.mode = history
		case "d":
			if !m.viewingPriorVersion() {
				m.mode = confirmDelete
			}
		}
	case editFields:
		fields := sortedKeys(m.draft)
		switch key.String() {
		case "esc":
			m.mode = view
		case "up", "k":
			if m.fieldCursor > 0 {
				m.fieldCursor--
			}
		case "down":
			if m.fieldCursor+1 < len(fields) {
				m.fieldCursor++
			}
		case "enter":
			if len(fields) > 0 {
				m.startFieldEdit(fields[m.fieldCursor], m.draft[fields[m.fieldCursor]])
				return m, textinput.Blink
			}
		case "a":
			m.startFieldEdit("", "value")
			return m, textinput.Blink
		case "d":
			if len(fields) > 0 {
				delete(m.draft, fields[m.fieldCursor])
				m.fieldCursor = min(m.fieldCursor, max(0, len(fields)-2))
			}
		case "j":
			raw, _ := json.MarshalIndent(m.draft, "", "  ")
			m.editor.SetValue(string(raw))
			m.editor.Focus()
			m.mode = edit
			return m, textarea.Blink
		case "ctrl+s":
			m.loading = true
			return m, m.writeSecret(m.selected, m.draft)
		}
	case editField:
		switch key.String() {
		case "esc":
			m.fieldName.Blur()
			m.fieldValue.Blur()
			m.mode = editFields
			return m, nil
		case "tab", "shift+tab":
			m.fieldNameFocus = !m.fieldNameFocus
			if m.fieldNameFocus {
				m.fieldValue.Blur()
				m.fieldName.Focus()
				return m, textinput.Blink
			}
			m.fieldName.Blur()
			m.fieldValue.Focus()
			return m, textarea.Blink
		case "ctrl+s":
			if err := m.applyFieldEdit(); err != nil {
				m.err = err
				return m, nil
			}
			return m, nil
		}
		var cmd tea.Cmd
		if m.fieldNameFocus {
			m.fieldName, cmd = m.fieldName.Update(key)
		} else {
			m.fieldValue, cmd = m.fieldValue.Update(key)
		}
		return m, cmd
	case edit:
		if key.String() == "esc" {
			m.editor.Blur()
			m.mode = editFields
			return m, nil
		}
		if key.String() == "ctrl+s" {
			data, err := decodeSecretJSON(m.editor.Value())
			if err != nil {
				m.err = fmt.Errorf("invalid JSON: %w", err)
				return m, nil
			}
			m.draft = data
			m.loading = true
			m.editor.Blur()
			return m, m.writeSecret(m.selected, data)
		}
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(key)
		return m, cmd
	case addName:
		if key.String() == "esc" {
			m.name.Blur()
			m.mode = browse
			return m, nil
		}
		if key.String() == "enter" {
			secretName := strings.Trim(m.name.Value(), "/")
			if secretName == "" {
				m.err = fmt.Errorf("secret path cannot be empty")
				return m, nil
			}
			m.selected = secretName
			m.data = make(map[string]any)
			m.name.Blur()
			m.startStructuredEdit()
			m.startFieldEdit("", "value")
			return m, textinput.Blink
		}
		var cmd tea.Cmd
		m.name, cmd = m.name.Update(key)
		return m, cmd
	case confirmDelete:
		switch strings.ToLower(key.String()) {
		case "y":
			m.loading = true
			return m, m.deleteSecret(m.selected)
		case "n", "esc":
			m.mode = view
		}
	case history:
		switch key.String() {
		case "esc", "q":
			m.mode = view
		case "up", "k":
			if m.historyCursor > 0 {
				m.historyCursor--
			}
		case "down", "j":
			if m.historyCursor+1 < len(m.metadata.Versions) {
				m.historyCursor++
			}
		case "enter":
			if version, ok := m.selectedVersion(); ok && !version.Destroyed && version.DeletionTime == nil {
				m.loading = true
				return m, m.loadSecret(m.selected, version.Version)
			}
		case "r":
			if version, ok := m.selectedVersion(); ok && !version.Destroyed && version.DeletionTime == nil {
				m.mode = confirmRestore
			}
		case "u":
			if version, ok := m.selectedVersion(); ok && !version.Destroyed && version.DeletionTime != nil {
				m.loading = true
				return m, m.undeleteSecret(m.selected, version.Version)
			}
		case "x":
			if version, ok := m.selectedVersion(); ok && !version.Destroyed {
				m.destroyConfirm.SetValue("")
				m.destroyConfirm.Placeholder = fmt.Sprintf("destroy v%d", version.Version)
				m.destroyConfirm.Focus()
				m.mode = confirmDestroy
				return m, textinput.Blink
			}
		}
	case confirmRestore:
		switch strings.ToLower(key.String()) {
		case "y":
			if version, ok := m.selectedVersion(); ok {
				m.loading = true
				return m, m.restoreSecret(m.selected, version.Version)
			}
		case "n", "esc":
			m.mode = history
		}
	case confirmDestroy:
		switch key.String() {
		case "esc":
			m.destroyConfirm.Blur()
			m.mode = history
		case "enter":
			version, ok := m.selectedVersion()
			if !ok {
				m.mode = history
				break
			}
			expected := fmt.Sprintf("destroy v%d", version.Version)
			if m.destroyConfirm.Value() != expected {
				m.err = fmt.Errorf("type %q exactly to confirm permanent destruction", expected)
				return m, nil
			}
			m.destroyConfirm.Blur()
			m.loading = true
			return m, m.destroySecret(m.selected, version.Version)
		default:
			var cmd tea.Cmd
			m.destroyConfirm, cmd = m.destroyConfirm.Update(key)
			return m, cmd
		}
	case directPath:
		if key.String() == "esc" {
			m.pathInput.Blur()
			m.mode = browse
			return m, nil
		}
		if key.String() == "enter" {
			m.pathInput.Blur()
			m.clearFilter()
			m.loading = true
			m.mode = browse
			return m, m.loadListAt(normalizePath(m.pathInput.Value()))
		}
		var cmd tea.Cmd
		m.pathInput, cmd = m.pathInput.Update(key)
		return m, cmd
	case recentPaths:
		return m.handlePicker(key, m.recent, false)
	case mountPicker:
		return m.handlePicker(key, m.secrets.Mounts(), true)
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.singleLine(titleStyle, "VAUI — Vault secrets"))
	b.WriteString("\n")
	namespace := m.secrets.Namespace()
	if namespace == "" {
		namespace = "(root)"
	}
	b.WriteString(m.singleLine(dimStyle, fmt.Sprintf("Vault: %s • Namespace: %s • Mount: %s", m.secrets.Address(), namespace, m.secrets.Mount())) + "\n")
	if m.loading {
		b.WriteString(dimStyle.Render("Working…"))
		b.WriteString("\n")
	}
	if m.err != nil {
		b.WriteString(m.errorView())
		b.WriteString("\n")
	}
	if m.status != "" {
		b.WriteString(m.status)
		b.WriteString("\n")
	}
	switch m.mode {
	case browse, filtering:
		b.WriteString(m.singleLine(lipgloss.NewStyle(), "Path: "+breadcrumb(m.prefix)) + "\n\n")
		if m.mode == filtering {
			b.WriteString("Filter: " + m.filter.View() + "\n\n")
		} else if m.filter.Value() != "" {
			b.WriteString("Filter: " + m.filter.Value() + "\n\n")
		}
		visible := m.visibleKeys()
		if len(visible) == 0 && !m.loading {
			emptyMessage := "No secrets here."
			if m.filter.Value() != "" {
				emptyMessage = "No matching paths."
			}
			b.WriteString(dimStyle.Render(emptyMessage))
			b.WriteString("\n")
		}
		start, end := m.visibleRange(len(visible))
		for i := start; i < end; i++ {
			key := visible[i]
			marker := "  "
			style := lipgloss.NewStyle()
			if i == m.cursor {
				marker = "> "
				style = selectedStyle
			}
			b.WriteString(m.singleLine(style, marker+key))
			b.WriteString("\n")
		}
		if m.mode == filtering {
			b.WriteString("\n" + m.singleLine(dimStyle, fmt.Sprintf("%s • type to filter • enter apply • esc clear", m.position(len(visible)))))
		} else {
			b.WriteString("\n" + m.singleLine(dimStyle, fmt.Sprintf("%s • ↑/↓ navigate • enter open • g go to • p recent • m mounts • backspace parent • a add • q quit", m.position(len(visible)))))
		}
	case view:
		b.WriteString("\nSecret: " + m.selected)
		if m.value.Version > 0 {
			b.WriteString(fmt.Sprintf(" • version %d", m.value.Version))
		}
		if !m.value.CreatedTime.IsZero() {
			b.WriteString(" • created " + formatTime(m.value.CreatedTime))
		}
		b.WriteString("\n\n")
		fields := m.secretKeys()
		if len(fields) == 0 {
			b.WriteString(dimStyle.Render("No fields."))
			b.WriteString("\n")
		}
		for i, field := range fields {
			marker := "  "
			style := lipgloss.NewStyle()
			if i == m.fieldCursor {
				marker = "> "
				style = selectedStyle
			}
			value := "••••••••"
			if m.revealed[field] {
				value = displayValue(m.data[field])
			}
			b.WriteString(style.Render(fmt.Sprintf("%s%s: %s", marker, field, value)))
			b.WriteString("\n")
		}
		help := "↑/↓ select • r reveal/hide • c copy • e edit • h history • d delete • esc back"
		if m.viewingPriorVersion() {
			help = "↑/↓ select • r reveal/hide • c copy • h history • esc back"
		}
		b.WriteString("\n" + dimStyle.Render(help))
	case editFields:
		b.WriteString("\nEditing fields: " + m.selected + "\n\n")
		fields := sortedKeys(m.draft)
		if len(fields) == 0 {
			b.WriteString(dimStyle.Render("No fields. Press a to add one."))
			b.WriteString("\n")
		}
		for i, field := range fields {
			marker := "  "
			style := lipgloss.NewStyle()
			if i == m.fieldCursor {
				marker = "> "
				style = selectedStyle
			}
			b.WriteString(style.Render(fmt.Sprintf("%s%s: %s", marker, field, displayValue(m.draft[field]))))
			b.WriteString("\n")
		}
		b.WriteString("\n" + dimStyle.Render("↑/↓ select • enter edit • a add • d remove • j raw JSON • ctrl+s save • esc cancel"))
	case editField:
		b.WriteString("\nEdit field: " + m.selected + "\n\n")
		b.WriteString("Name:\n" + m.fieldName.View() + "\n\n")
		b.WriteString("JSON value:\n" + m.fieldValue.View())
		b.WriteString("\n" + dimStyle.Render("tab switch field • ctrl+s apply • esc cancel"))
	case edit:
		b.WriteString("\nRaw JSON: " + m.selected + "\n")
		b.WriteString(m.editor.View())
		b.WriteString("\n" + dimStyle.Render("ctrl+s save • esc structured editor"))
	case addName:
		b.WriteString("\nNew secret path:\n")
		b.WriteString(m.name.View())
		b.WriteString("\n" + dimStyle.Render("enter continue • esc cancel"))
	case confirmDelete:
		b.WriteString("\nSoft-delete the current version of " + selectedStyle.Render(m.selected) + "? It can be undeleted from history. (y/N)")
	case history:
		b.WriteString("\nVersion history: " + m.selected + "\n\n")
		if m.historyErr != nil {
			b.WriteString(errorStyle.Render("Version history unavailable: "+m.historyErr.Error()) + "\n")
		} else {
			for i, version := range m.metadata.Versions {
				marker, label := "  ", fmt.Sprintf("v%d • %s", version.Version, formatTime(version.CreatedTime))
				if version.Version == m.metadata.CurrentVersion {
					label += " • current"
				}
				if version.Destroyed {
					label += " • permanently destroyed"
				} else if version.DeletionTime != nil {
					label += " • soft-deleted"
				} else {
					label += " • active"
				}
				style := lipgloss.NewStyle()
				if i == m.historyCursor {
					marker, style = "> ", selectedStyle
				}
				b.WriteString(style.Render(marker+label) + "\n")
			}
		}
		b.WriteString("\n" + dimStyle.Render("↑/↓ select • enter inspect • r restore • u undelete • x permanently destroy • esc back"))
	case confirmRestore:
		version, _ := m.selectedVersion()
		b.WriteString(fmt.Sprintf("\nRestore version %d of %s as a new current version? (y/N)", version.Version, selectedStyle.Render(m.selected)))
	case confirmDestroy:
		version, _ := m.selectedVersion()
		b.WriteString(fmt.Sprintf("\nPermanently destroy version %d of %s? This cannot be undone.\n\nType %q and press enter:\n%s\n", version.Version, selectedStyle.Render(m.selected), fmt.Sprintf("destroy v%d", version.Version), m.destroyConfirm.View()))
		b.WriteString(dimStyle.Render("esc cancel"))
	case directPath:
		b.WriteString("\nGo directly to path:\n" + m.pathInput.View() + "\n" + dimStyle.Render("enter open • esc cancel"))
	case recentPaths:
		b.WriteString(m.pickerView("Recent paths", m.recent))
	case mountPicker:
		b.WriteString(m.pickerView("Select mount", m.secrets.Mounts()))
	case help:
		b.WriteString("\n" + titleStyle.Render("Help") + "\n\n")
		b.WriteString(m.helpText(m.helpReturn) + "\n\n" + dimStyle.Render("? or esc close"))
	}
	return b.String()
}

func (m Model) helpText(current mode) string {
	common := "? help • ctrl+c quit"
	var controls string
	switch current {
	case browse:
		controls = "↑/↓ select • enter open • g go to path • p recent paths • m switch mount • / filter • a add • backspace parent"
	case filtering:
		controls = "type to filter • enter apply • esc clear"
	case view:
		controls = "↑/↓ select field • r reveal • c copy • e edit • h history • d soft-delete • esc back"
	case editFields:
		controls = "↑/↓ select • enter edit • a add • d remove • j raw JSON • ctrl+s save • esc cancel"
	case editField:
		controls = "tab switch input • ctrl+s apply • esc cancel"
	case edit:
		controls = "edit raw JSON • ctrl+s save • esc structured editor"
	case history:
		controls = "↑/↓ select • enter inspect • r restore • u undelete • x permanently destroy • esc back"
	case directPath:
		controls = "enter open path • esc cancel"
	case recentPaths, mountPicker:
		controls = "↑/↓ select • enter choose • esc cancel"
	default:
		controls = "follow the prompt • esc cancel"
	}
	return controls + "\n" + common
}

func (m Model) loadList() tea.Cmd {
	return m.loadListAt(m.prefix)
}
func (m Model) loadListAt(prefix string) tea.Cmd {
	return func() tea.Msg {
		keys, err := m.secrets.List(context.Background(), prefix)
		return listMsg{prefix: prefix, keys: keys, err: err}
	}
}
func (m Model) loadSecret(name string, version int) tea.Cmd {
	return func() tea.Msg {
		value, err := m.secrets.Read(context.Background(), name, version)
		return readMsg{name: name, value: value, version: version, err: err}
	}
}
func (m Model) loadMetadata(name string) tea.Cmd {
	return func() tea.Msg {
		value, err := m.secrets.Metadata(context.Background(), name)
		return metadataMsg{value, err}
	}
}
func (m Model) restoreSecret(name string, version int) tea.Cmd {
	return func() tea.Msg { return restoreMsg{version, m.secrets.Restore(context.Background(), name, version)} }
}
func (m Model) writeSecret(name string, data map[string]any) tea.Cmd {
	return func() tea.Msg {
		err := m.secrets.Write(context.Background(), name, data)
		return actionMsg{"Saved " + name, err}
	}
}
func (m Model) selectedVersion() (secret.Version, bool) {
	if m.historyCursor < 0 || m.historyCursor >= len(m.metadata.Versions) {
		return secret.Version{}, false
	}
	return m.metadata.Versions[m.historyCursor], true
}
func (m Model) viewingPriorVersion() bool {
	return m.metadata.CurrentVersion > 0 && m.value.Version > 0 && m.value.Version != m.metadata.CurrentVersion
}
func formatTime(value time.Time) string { return value.Local().Format("2006-01-02 15:04 MST") }
func (m Model) deleteSecret(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.secrets.Delete(context.Background(), name)
		if err != nil {
			err = fmt.Errorf("soft-delete current version of %s: %w", name, err)
		}
		return versionActionMsg{"Soft-deleted current version of " + name, err}
	}
}
func (m Model) undeleteSecret(name string, version int) tea.Cmd {
	return func() tea.Msg {
		err := m.secrets.Undelete(context.Background(), name, version)
		if err != nil {
			err = fmt.Errorf("undelete version %d of %s: %w", version, name, err)
		}
		return versionActionMsg{fmt.Sprintf("Undeleted version %d of %s", version, name), err}
	}
}
func (m Model) destroySecret(name string, version int) tea.Cmd {
	return func() tea.Msg {
		err := m.secrets.Destroy(context.Background(), name, version)
		if err != nil {
			err = fmt.Errorf("permanently destroy version %d of %s: %w", version, name, err)
		}
		return versionActionMsg{fmt.Sprintf("Permanently destroyed version %d of %s", version, name), err}
	}
}
func (m Model) copySelectedValue(key string) tea.Cmd {
	value, err := clipboardValue(m.data[key])
	if err != nil {
		return func() tea.Msg { return copyMsg{key: key, err: fmt.Errorf("copy value for %s: %w", key, err)} }
	}
	copyValue := m.copyValue
	return func() tea.Msg {
		if err := copyValue(value); err != nil {
			return copyMsg{key: key, err: fmt.Errorf("copy value for %s: %w", key, err)}
		}
		return copyMsg{key: key}
	}
}
func (m Model) secretKeys() []string {
	return sortedKeys(m.data)
}
func sortedKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func (m *Model) startStructuredEdit() {
	m.draft = make(map[string]any, len(m.data))
	for key, value := range m.data {
		m.draft[key] = value
	}
	m.fieldCursor = min(m.fieldCursor, max(0, len(m.draft)-1))
	m.mode = editFields
}
func (m *Model) startFieldEdit(key string, value any) {
	m.originalField = key
	m.fieldName.SetValue(key)
	m.fieldName.CursorEnd()
	m.fieldValue.SetValue(displayValue(value))
	m.fieldNameFocus = true
	m.fieldValue.Blur()
	m.fieldName.Focus()
	m.mode = editField
}
func (m *Model) applyFieldEdit() error {
	key := m.fieldName.Value()
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("field name cannot be empty")
	}
	if key != m.originalField {
		if _, exists := m.draft[key]; exists {
			return fmt.Errorf("field %q already exists", key)
		}
	}
	value, err := decodeJSON(m.fieldValue.Value())
	if err != nil {
		return fmt.Errorf("invalid JSON value: %w", err)
	}
	if m.originalField != "" && key != m.originalField {
		delete(m.draft, m.originalField)
	}
	m.draft[key] = value
	for i, field := range sortedKeys(m.draft) {
		if field == key {
			m.fieldCursor = i
			break
		}
	}
	m.fieldName.Blur()
	m.fieldValue.Blur()
	m.mode = editFields
	return nil
}
func decodeSecretJSON(raw string) (map[string]any, error) {
	value, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	data, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("secret must be a JSON object")
	}
	return data, nil
}
func decodeJSON(raw string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	value, err := decodeJSONToken(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, err
	}
	return value, nil
}
func decodeJSONToken(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate field %q", key)
			}
			value, err := decodeJSONToken(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		var array []any
		for decoder.More() {
			value, err := decodeJSONToken(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}
func displayValue(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "<unavailable>"
	}
	return string(raw)
}
func clipboardValue(value any) (string, error) {
	if value, ok := value.(string); ok {
		return value, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode value: %w", err)
	}
	return string(raw), nil
}
func (m Model) visibleKeys() []string {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if query == "" {
		return m.keys
	}
	keys := make([]string, 0, len(m.keys))
	for _, key := range m.keys {
		if strings.Contains(strings.ToLower(key), query) {
			keys = append(keys, key)
		}
	}
	return keys
}
func (m *Model) clearFilter() {
	m.filter.SetValue("")
	m.filter.Blur()
	m.cursor = 0
	m.listOffset = 0
}
func (m Model) listHeight() int {
	if m.height <= 0 {
		return len(m.visibleKeys())
	}
	fixedLines := 6 // title, mount, path and spacing, footer and spacing
	if m.loading {
		fixedLines++
	}
	if m.err != nil {
		fixedLines += lipgloss.Height(m.errorView())
	}
	if m.status != "" {
		fixedLines++
	}
	if m.mode == filtering || m.filter.Value() != "" {
		fixedLines += 2
	}
	return max(1, m.height-fixedLines)
}
func (m Model) errorView() string {
	style := errorStyle
	if m.width > 0 {
		style = style.Width(m.width)
	}
	return style.Render(m.err.Error())
}
func (m Model) singleLine(style lipgloss.Style, value string) string {
	if m.width > 0 {
		style = style.MaxWidth(m.width)
	}
	return style.Render(value)
}
func (m Model) visibleRange(length int) (int, int) {
	if length == 0 {
		return 0, 0
	}
	start := min(m.listOffset, length-1)
	return start, min(length, start+m.listHeight())
}
func (m *Model) ensureCursorVisible() {
	length := len(m.visibleKeys())
	if length == 0 {
		m.cursor = 0
		m.listOffset = 0
		return
	}
	m.cursor = min(max(0, m.cursor), length-1)
	height := max(1, m.listHeight())
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	} else if m.cursor >= m.listOffset+height {
		m.listOffset = m.cursor - height + 1
	}
	m.listOffset = min(m.listOffset, max(0, length-height))
}
func (m Model) position(length int) string {
	if length == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", m.cursor+1, length)
}
func parent(value string) string {
	parts := strings.Split(strings.TrimSuffix(value, "/"), "/")
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/") + "/"
}

func normalizePath(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	return path.Clean(value) + "/"
}
func breadcrumb(value string) string {
	if value == "" {
		return "/"
	}
	return "/ › " + strings.Join(strings.Split(strings.TrimSuffix(value, "/"), "/"), " › ")
}
func (m *Model) visit(path string) {
	path = normalizePath(path)
	if path == "" {
		return
	}
	next := []string{path}
	for _, old := range m.recent {
		if old != path {
			next = append(next, old)
		}
	}
	if len(next) > 10 {
		next = next[:10]
	}
	m.recent = next
}
func (m Model) handlePicker(key tea.KeyMsg, options []string, mounts bool) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.mode = browse
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
	case "down", "j":
		if m.pickerCursor+1 < len(options) {
			m.pickerCursor++
		}
	case "enter":
		if len(options) > 0 {
			target := options[m.pickerCursor]
			if mounts {
				if err := m.secrets.SelectMount(target); err != nil {
					m.err = err
					return m, nil
				}
				m.prefix = ""
				m.recent = nil
				target = ""
			}
			m.clearFilter()
			m.mode = browse
			m.loading = true
			return m, m.loadListAt(target)
		}
	}
	return m, nil
}
func (m Model) pickerView(title string, options []string) string {
	var b strings.Builder
	b.WriteString("\n" + title + ":\n\n")
	for i, v := range options {
		marker := "  "
		if i == m.pickerCursor {
			marker = "> "
		}
		b.WriteString(marker + v + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ select • enter open • esc cancel"))
	return b.String()
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
