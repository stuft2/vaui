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
	selected      string
	data          map[string]any
	status        string
	err           error
	loading       bool
	width, height int
	editor        textarea.Model
	name          textinput.Model
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
	return Model{secrets: secrets, loading: true, editor: ed, name: name}
}

func (m Model) Init() tea.Cmd { return m.loadList() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.editor.SetWidth(max(30, min(100, msg.Width-4)))
		m.editor.SetHeight(max(6, msg.Height-9))
	case listMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.keys = msg.keys
			if m.cursor >= len(m.keys) {
				m.cursor = max(0, len(m.keys)-1)
			}
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
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor+1 < len(m.keys) {
				m.cursor++
			}
		case "backspace", "h":
			if m.prefix != "" {
				m.prefix = parent(m.prefix)
				m.cursor = 0
				m.loading = true
				return m, m.loadList()
			}
		case "enter", "l", "v":
			if len(m.keys) == 0 {
				break
			}
			item := m.keys[m.cursor]
			if strings.HasSuffix(item, "/") {
				m.prefix += item
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
	case browse:
		location := "/"
		if m.prefix != "" {
			location += m.prefix
		}
		b.WriteString("Path: " + location + "\n\n")
		if len(m.keys) == 0 && !m.loading {
			b.WriteString(dimStyle.Render("No secrets here."))
			b.WriteString("\n")
		}
		for i, key := range m.keys {
			marker := "  "
			style := lipgloss.NewStyle()
			if i == m.cursor {
				marker = "> "
				style = selectedStyle
			}
			b.WriteString(style.Render(marker + key))
			b.WriteString("\n")
		}
		b.WriteString("\n" + dimStyle.Render("↑/↓ navigate • enter open • backspace parent • a add • q quit"))
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
