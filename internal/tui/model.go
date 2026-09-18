package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SecretStore describes the Vault operations the terminal UI consumes.
type SecretStore interface {
	List(context.Context, string) ([]string, error)
	Read(context.Context, string) (map[string]any, error)
	Write(context.Context, string, map[string]any) error
	Delete(context.Context, string) error
}

type mode int

const (
	browse mode = iota
	filtering
	view
	edit
	addName
	confirmDelete
)

type listMsg struct {
	keys []string
	err  error
}
type readMsg struct {
	name string
	data map[string]any
	err  error
}
type actionMsg struct {
	text string
	err  error
}

type Model struct {
	secrets       SecretStore
	mode          mode
	prefix        string
	keys          []string
	cursor        int
	listOffset    int
	selected      string
	data          map[string]any
	status        string
	err           error
	loading       bool
	width, height int
	editor        textarea.Model
	name          textinput.Model
	filter        textinput.Model
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
	return Model{secrets: secrets, loading: true, editor: ed, name: name, filter: filter}
}

func (m Model) Init() tea.Cmd { return m.loadList() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.editor.SetWidth(max(30, min(100, msg.Width-4)))
		m.editor.SetHeight(max(6, msg.Height-9))
		m.ensureCursorVisible()
	case listMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.keys = msg.keys
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
			m.selected, m.data, m.mode = msg.name, msg.data, view
		}
	case actionMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.text
			m.mode = browse
			return m, m.loadList()
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
				m.prefix = parent(m.prefix)
				m.clearFilter()
				m.cursor = 0
				m.loading = true
				return m, m.loadList()
			}
		case "enter", "l", "v":
			if len(visible) == 0 {
				break
			}
			item := visible[m.cursor]
			if strings.HasSuffix(item, "/") {
				m.prefix += item
				m.clearFilter()
				m.cursor = 0
				m.loading = true
				return m, m.loadList()
			}
			m.loading = true
			return m, m.loadSecret(m.prefix + item)
		case "a":
			m.mode = addName
			m.name.SetValue(strings.TrimSuffix(m.prefix, "/"))
			m.name.CursorEnd()
			m.name.Focus()
			return m, textinput.Blink
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
		switch key.String() {
		case "esc", "q":
			m.mode = browse
		case "e":
			raw, _ := json.MarshalIndent(m.data, "", "  ")
			m.editor.SetValue(string(raw))
			m.editor.Focus()
			m.mode = edit
			return m, textarea.Blink
		case "d":
			m.mode = confirmDelete
		}
	case edit:
		if key.String() == "esc" {
			m.editor.Blur()
			m.mode = view
			return m, nil
		}
		if key.String() == "ctrl+s" {
			var data map[string]any
			if err := json.Unmarshal([]byte(m.editor.Value()), &data); err != nil {
				m.err = fmt.Errorf("invalid JSON: %w", err)
				return m, nil
			}
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
			m.editor.SetValue("{\n  \"key\": \"value\"\n}")
			m.editor.Focus()
			m.name.Blur()
			m.mode = edit
			return m, textarea.Blink
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
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("VAUI — Vault secrets"))
	b.WriteString("\n")
	if m.loading {
		b.WriteString(dimStyle.Render("Working…"))
		b.WriteString("\n")
	}
	if m.err != nil {
		b.WriteString(errorStyle.Render(m.err.Error()))
		b.WriteString("\n")
	}
	if m.status != "" {
		b.WriteString(m.status)
		b.WriteString("\n")
	}
	switch m.mode {
	case browse, filtering:
		location := "/"
		if m.prefix != "" {
			location += m.prefix
		}
		b.WriteString("Path: " + location + "\n\n")
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
			b.WriteString(style.Render(marker + key))
			b.WriteString("\n")
		}
		if m.mode == filtering {
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("%s • type to filter • enter apply • esc clear", m.position(len(visible)))))
		} else {
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("%s • ↑/↓ navigate • enter open • / filter • backspace parent • a add • q quit", m.position(len(visible)))))
		}
	case view:
		b.WriteString("\nSecret: " + m.selected + "\n\n")
		raw, _ := json.MarshalIndent(m.data, "", "  ")
		b.Write(raw)
		b.WriteString("\n\n" + dimStyle.Render("e edit • d delete • esc back"))
	case edit:
		b.WriteString("\nEditing: " + m.selected + "\n")
		b.WriteString(m.editor.View())
		b.WriteString("\n" + dimStyle.Render("ctrl+s save • esc cancel"))
	case addName:
		b.WriteString("\nNew secret path:\n")
		b.WriteString(m.name.View())
		b.WriteString("\n" + dimStyle.Render("enter continue • esc cancel"))
	case confirmDelete:
		b.WriteString("\nDelete all versions and metadata for " + selectedStyle.Render(m.selected) + "? (y/N)")
	}
	return b.String()
}

func (m Model) loadList() tea.Cmd {
	prefix := m.prefix
	return func() tea.Msg { keys, err := m.secrets.List(context.Background(), prefix); return listMsg{keys, err} }
}
func (m Model) loadSecret(name string) tea.Cmd {
	return func() tea.Msg {
		data, err := m.secrets.Read(context.Background(), name)
		return readMsg{name, data, err}
	}
}
func (m Model) writeSecret(name string, data map[string]any) tea.Cmd {
	return func() tea.Msg {
		err := m.secrets.Write(context.Background(), name, data)
		return actionMsg{"Saved " + name, err}
	}
}
func (m Model) deleteSecret(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.secrets.Delete(context.Background(), name)
		return actionMsg{"Deleted " + name, err}
	}
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
	fixedLines := 5 // title, path and spacing, footer and spacing
	if m.loading {
		fixedLines++
	}
	if m.err != nil {
		fixedLines++
	}
	if m.status != "" {
		fixedLines++
	}
	if m.mode == filtering || m.filter.Value() != "" {
		fixedLines += 2
	}
	return max(1, m.height-fixedLines)
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
