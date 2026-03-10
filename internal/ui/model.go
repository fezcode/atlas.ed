package ui

import (
	"fmt"
	"os"
	"strings"

	"atlas.ed/internal/editor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#D4AF37")).
			Padding(0, 1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D4AF37")).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	matchCountStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#D4AF37")).
			Padding(0, 1).
			Bold(true)
			
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#555555")).
			Padding(0, 1)

	modeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#D4AF37")).
			Bold(true).
			Padding(0, 1)

	confirmStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#D4AF37")).
			Padding(1, 2)
)

type Mode int

const (
	ModeEdit Mode = iota
	ModeSearchInput
	ModeSearchNav
	ModeQuitConfirm
)

type Model struct {
	filename        string
	initialContent  string
	textarea        textarea.Model
	searchInput     textinput.Model
	viewport        viewport.Model
	mode            Mode
	showLineNumbers bool
	modified        bool
	
	// Search
	searchQuery string
	matches     []int
	matchIndex  int
	
	width  int
	height int
}

func NewModel(filename string, content string) Model {
	ta := textarea.New()
	ta.Placeholder = "Start typing..."
	ta.SetValue(content)
	ta.Focus()
	ta.ShowLineNumbers = true

	si := textinput.New()
	si.Placeholder = "Search query..."
	si.Prompt = " / "

	vp := viewport.New(80, 20)

	return Model{
		filename:        filename,
		initialContent:  content,
		textarea:        ta,
		searchInput:     si,
		viewport:        vp,
		mode:            ModeEdit,
		showLineNumbers: true,
		matchIndex:      -1,
	}
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global Quit/Interrupt handling
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" {
			if m.modified {
				m.mode = ModeQuitConfirm
				return m, nil
			}
			return m, tea.Quit
		}

		// Handle Quit Confirmation
		if m.mode == ModeQuitConfirm {
			switch msg.String() {
			case "y", "Y":
				m.saveFile()
				return m, tea.Quit
			case "n", "N":
				return m, tea.Quit
			case "esc":
				m.mode = ModeEdit
				return m, nil
			}
			return m, nil
		}

		// Handle Search Input Mode
		if m.mode == ModeSearchInput {
			switch msg.String() {
			case "enter":
				m.searchQuery = m.searchInput.Value()
				if m.searchQuery == "" {
					m.mode = ModeEdit
					return m, nil
				}
				m.performSearch()
				if len(m.matches) > 0 {
					m.mode = ModeSearchNav
					m.updateViewport()
				} else {
					m.mode = ModeEdit
				}
				return m, nil
			case "esc":
				m.mode = ModeEdit
				return m, nil
			}
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}

		// Handle Search Navigation Mode
		if m.mode == ModeSearchNav {
			switch msg.String() {
			case "enter":
				m.mode = ModeEdit
				return m, nil
			case "n":
				m.findNext()
				m.updateViewport()
				return m, nil
			case "p", "N":
				m.findPrev()
				m.updateViewport()
				return m, nil
			case "esc", "q":
				m.mode = ModeEdit
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

		// Handle Edit Mode
		switch msg.String() {
		case "ctrl+s":
			m.saveFile()
			return m, nil
		case "ctrl+f":
			m.mode = ModeSearchInput
			m.searchInput.Focus()
			m.searchInput.SetValue("")
			return m, textinput.Blink
		case "ctrl+l":
			m.showLineNumbers = !m.showLineNumbers
			m.textarea.ShowLineNumbers = m.showLineNumbers
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		
		m.textarea.SetWidth(msg.Width)
		m.textarea.SetHeight(msg.Height - headerHeight - footerHeight)
		
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - headerHeight - footerHeight
	}

	if m.mode == ModeEdit {
		var taCmd tea.Cmd
		prevVal := m.textarea.Value()
		m.textarea, taCmd = m.textarea.Update(msg)
		if m.textarea.Value() != prevVal {
			m.modified = true
		}
		cmds = append(cmds, taCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateViewport() {
	content := m.textarea.Value()
	highlighted, _ := editor.Highlight(content, m.filename)
	final := editor.HighlightSearch(highlighted, m.searchQuery, m.matchIndex)
	
	if m.showLineNumbers {
		lines := strings.Split(final, "\n")
		width := len(fmt.Sprintf("%d", len(lines)))
		for i, line := range lines {
			num := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render(fmt.Sprintf("%*d ", width, i+1))
			lines[i] = num + line
		}
		final = strings.Join(lines, "\n")
	}
	m.viewport.SetContent(final)
	
	offset := m.matches[m.matchIndex]
	plain := m.textarea.Value()
	lineNum := strings.Count(plain[:offset], "\n")
	m.viewport.SetYOffset(lineNum)
}

func (m *Model) saveFile() {
	_ = os.WriteFile(m.filename, []byte(m.textarea.Value()), 0644)
	m.modified = false
}

func (m *Model) performSearch() {
	m.matches = nil
	content := strings.ToLower(m.textarea.Value())
	query := strings.ToLower(m.searchQuery)
	start := 0
	for {
		idx := strings.Index(content[start:], query)
		if idx == -1 { break }
		m.matches = append(m.matches, start+idx)
		start += idx + len(query)
	}
	if len(m.matches) > 0 { 
		m.matchIndex = 0 
		m.jumpToMatch()
	} else { 
		m.matchIndex = -1 
	}
}

func (m *Model) findNext() {
	if len(m.matches) == 0 { return }
	m.matchIndex = (m.matchIndex + 1) % len(m.matches)
	m.jumpToMatch()
}

func (m *Model) findPrev() {
	if len(m.matches) == 0 { return }
	m.matchIndex = (m.matchIndex - 1 + len(m.matches)) % len(m.matches)
	m.jumpToMatch()
}

func (m *Model) jumpToMatch() {
	if m.matchIndex < 0 { return }
	
	offset := m.matches[m.matchIndex]
	plain := m.textarea.Value()

	targetLine := strings.Count(plain[:offset], "\n")
	
	lastNewLine := strings.LastIndex(plain[:offset], "\n")
	targetCol := 0
	if lastNewLine == -1 {
		targetCol = len([]rune(plain[:offset]))
	} else {
		targetCol = len([]rune(plain[lastNewLine+1 : offset]))
	}

	for m.textarea.Line() < targetLine {
		m.textarea.CursorDown()
	}
	for m.textarea.Line() > targetLine {
		m.textarea.CursorUp()
	}
	
	m.textarea.SetCursor(targetCol)
}

func (m Model) View() string {
	var body string
	if m.mode == ModeSearchNav {
		body = m.viewport.View()
	} else {
		body = m.textarea.View()
	}
	
	view := fmt.Sprintf("%s\n%s\n%s", m.headerView(), body, m.footerView())
	
	if m.mode == ModeQuitConfirm {
		dialog := confirmStyle.Render("Unsaved changes! Save before quitting?\n\n(y)es / (n)o / (esc) cancel")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
	}
	
	return view
}

func (m Model) headerView() string {
	var currentMode string
	switch m.mode {
	case ModeSearchNav:
		currentMode = " SEARCH MODE "
	case ModeSearchInput:
		currentMode = " INPUT QUERY "
	default:
		currentMode = " EDIT MODE "
	}
	
	modChar := ""
	if m.modified { modChar = "*" }
	
	title := titleStyle.Render("ATLAS ED")
	mLabel := modeStyle.Render(currentMode)
	status := statusStyle.Render(" " + m.filename + modChar + " ")
	
	line := strings.Repeat("─", max(0, m.width-lipgloss.Width(title)-lipgloss.Width(mLabel)-lipgloss.Width(status)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, mLabel, status, infoStyle.Render(line))
}

func (m Model) footerView() string {
	if m.mode == ModeSearchInput {
		return m.searchInput.View()
	}

	var help string
	if m.mode == ModeSearchNav {
		help = lipgloss.JoinHorizontal(lipgloss.Top,
			helpKeyStyle.Render(" enter "), helpDescStyle.Render("go to match "),
			helpKeyStyle.Render(" n/p "), helpDescStyle.Render("next/prev "),
			helpKeyStyle.Render(" q/esc "), helpDescStyle.Render("stop search "),
			helpKeyStyle.Render(" ^Q "), helpDescStyle.Render("quit "),
		)
	} else {
		help = lipgloss.JoinHorizontal(lipgloss.Top,
			helpKeyStyle.Render(" ^S "), helpDescStyle.Render("save "),
			helpKeyStyle.Render(" ^F "), helpDescStyle.Render("find "),
			helpKeyStyle.Render(" ^L "), helpDescStyle.Render("lines "),
			helpKeyStyle.Render(" ^Q "), helpDescStyle.Render("quit "),
		)
	}
	
	matchInfo := ""
	if len(m.matches) > 0 {
		matchInfo = matchCountStyle.Render(fmt.Sprintf(" MATCH %d/%d ", m.matchIndex+1, len(m.matches)))
	}

	gap := max(0, m.width-lipgloss.Width(help)-lipgloss.Width(matchInfo)-2)
	line := strings.Repeat(" ", gap)
	
	return lipgloss.JoinHorizontal(lipgloss.Center, help, line, matchInfo)
}

func max(a, b int) int {
	if a > b { return a }
	return b
}
