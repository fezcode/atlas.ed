package ui

import (
	"fmt"
	"os"
	"strings"

	"atlas.ed/internal/editor"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
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

type ActionType int

const (
	ActionNone ActionType = iota
	ActionInsert
	ActionDelete
	ActionOther
	ActionPaste
)

type Pos struct {
	Line int
	Col  int
}

type UndoState struct {
	Content string
	Line    int
	Col     int
}

type Model struct {
	filename        string
	initialContent  string
	textarea        textarea.Model
	searchInput     textinput.Model
	viewport        viewport.Model
	mode            Mode
	showLineNumbers bool
	modified        bool

	// Undo/Redo
	undoStack   []UndoState
	redoStack   []UndoState
	lastAction  ActionType
	actionCount int

	// Search

	searchQuery string
	matches     []int
	matchIndex  int
	
	lineEnding string // "\n" or "\r\n"

	// Selection
	selAnchor Pos
	selecting bool
	
	// Cache
	highlightedContent   string
	lastHighlightedValue string
	
	headerCache string
	footerCache string
	lastWidth   int
	lastMode    Mode
	lastModified bool
	lastMatchesCount int
	lastMatchIndex int
	lastLine int
	lastCol  int

	xOffset int
	width  int
	height int
}

func NewModel(filename string, content string) Model {
	le := "\n"
	if strings.Contains(content, "\r\n") {
		le = "\r\n"
	}

	// Normalize internally to LF
	content = strings.ReplaceAll(content, "\r\n", "\n")

	ta := textarea.New()
	ta.Placeholder = "Start typing..."
	ta.CharLimit = 0
	ta.SetWidth(1000000)
	ta.SetValue(content)
	ta.SetCursor(0) // Start at the top
	ta.Focus()
	ta.ShowLineNumbers = true
	ta.MaxHeight = 9999 // Ensure gutter width is consistent for up to 9999 lines
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#D4AF37"))
	ta.FocusedStyle.LineNumber = style
	ta.BlurredStyle.LineNumber = style
	ta.FocusedStyle.CursorLineNumber = style
	ta.BlurredStyle.CursorLineNumber = style

	ta.KeyMap.WordForward = key.NewBinding(
		key.WithKeys("ctrl+right", "alt+right", "alt+f"),
		key.WithHelp("ctrl+right", "word forward"),
	)
	ta.KeyMap.WordBackward = key.NewBinding(
		key.WithKeys("ctrl+left", "alt+left", "alt+b"),
		key.WithHelp("ctrl+left", "word backward"),
	)

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
		lineEnding:      le,
		selAnchor:       Pos{-1, -1},
		xOffset:         0,
	}
}

func (m *Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle Paste in Bubbletea v1
		if msg.Paste {
			if m.mode == ModeEdit {
				prevVal := m.textarea.Value()
				prevLine := m.textarea.Line()
				prevCol := m.textarea.LineInfo().CharOffset
				if m.hasSelection() {
					m.deleteSelectionInPlace()
				}
				text := msg.String()
				text = strings.ReplaceAll(text, "\r\n", "\n")
				m.textarea.InsertString(text)
				m.trackChange(prevVal, prevLine, prevCol, ActionPaste)
				return m, nil
			}
		}

		// Global Quit/Interrupt handling
		if msg.String() == "ctrl+q" {
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
		case "ctrl+z":
			m.undo()
			return m, nil
		case "ctrl+y":
			m.redo()
			return m, nil
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
		case "pgup":
			m.clearSelection()
			for i := 0; i < m.textarea.Height(); i++ {
				m.textarea.CursorUp()
			}
		case "pgdown":
			m.clearSelection()
			for i := 0; i < m.textarea.Height(); i++ {
				m.textarea.CursorDown()
			}
		case "home":
			m.clearSelection()
			m.textarea.CursorStart()
		case "end":
			m.clearSelection()
			m.textarea.CursorEnd()

		// Selection with shift+arrow
		case "shift+left":
			m.startOrExtendSelection()
			m.moveCursorLeft()
			return m, nil
		case "shift+right":
			m.startOrExtendSelection()
			m.moveCursorRight()
			return m, nil
		case "shift+up":
			m.startOrExtendSelection()
			m.textarea.CursorUp()
			return m, nil
		case "shift+down":
			m.startOrExtendSelection()
			m.textarea.CursorDown()
			return m, nil
		case "ctrl+shift+left":
			m.startOrExtendSelection()
			m.wordLeft()
			return m, nil
		case "ctrl+shift+right":
			m.startOrExtendSelection()
			m.wordRight()
			return m, nil
		case "shift+home":
			m.startOrExtendSelection()
			m.textarea.CursorStart()
			return m, nil
		case "shift+end":
			m.startOrExtendSelection()
			m.textarea.CursorEnd()
			return m, nil

		// Clipboard
		case "ctrl+c":
			if m.hasSelection() {
				clipboard.WriteAll(m.getSelectedText())
			}
			return m, nil
		case "ctrl+x":
			if m.hasSelection() {
				clipboard.WriteAll(m.getSelectedText())
				prevVal := m.textarea.Value()
				prevLine := m.textarea.Line()
				prevCol := m.textarea.LineInfo().CharOffset
				m.deleteSelectionInPlace()
				m.trackChange(prevVal, prevLine, prevCol, ActionOther)
			}
			return m, nil
		case "ctrl+v":
			text, err := clipboard.ReadAll()
			if err == nil && text != "" {
				text = strings.ReplaceAll(text, "\r\n", "\n")
				prevVal := m.textarea.Value()
				prevLine := m.textarea.Line()
				prevCol := m.textarea.LineInfo().CharOffset
				if m.hasSelection() {
					m.deleteSelectionInPlace()
				}
				m.textarea.InsertString(text)
				m.trackChange(prevVal, prevLine, prevCol, ActionPaste)
			}
			return m, nil
		case "ctrl+a":
			lines := strings.Split(m.textarea.Value(), "\n")
			m.selAnchor = Pos{0, 0}
			m.selecting = true
			lastLine := len(lines) - 1
			for m.textarea.Line() < lastLine {
				m.textarea.CursorDown()
			}
			m.textarea.CursorEnd()
			return m, nil
		case "tab":
			prevVal := m.textarea.Value()
			prevLine := m.textarea.Line()
			prevCol := m.textarea.LineInfo().CharOffset
			if m.hasSelection() {
				m.deleteSelectionInPlace()
			}
			m.textarea.InsertString("\t")
			m.trackChange(prevVal, prevLine, prevCol, ActionOther)
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		
		// Do NOT set textarea width to msg.Width, otherwise it will wrap.
		// We keep it large (9999) as initialized in NewModel.
		m.textarea.SetHeight(msg.Height - headerHeight - footerHeight)
		
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - headerHeight - footerHeight
	}

	if m.mode == ModeEdit {
		var taCmd tea.Cmd

		if kmsg, ok := msg.(tea.KeyMsg); ok {
			s := kmsg.String()
			// Keys that definitely don't change content (navigation, toggle, find, quit)
			isNav := s == "up" || s == "down" || s == "left" || s == "right" ||
				s == "ctrl+left" || s == "ctrl+right" ||
				s == "pgup" || s == "pgdown" || s == "home" || s == "end" ||
				s == "ctrl+s" || s == "ctrl+f" || s == "ctrl+l" || s == "ctrl+q" ||
				s == "ctrl+z" || s == "ctrl+y" || s == "esc" ||
				s == "ctrl+c" || s == "ctrl+x" || s == "ctrl+v" || s == "ctrl+a" ||
				strings.HasPrefix(s, "shift+") || strings.HasPrefix(s, "ctrl+shift+")

			if !isNav {
				prevVal := m.textarea.Value()
				prevLine := m.textarea.Line()
				prevCol := m.textarea.LineInfo().CharOffset
				skipTextarea := false
				// Only treat as content-modifying if it's a printable rune (>= space),
				// backspace, delete, enter, or tab.
				// On Windows, bare modifier keys (ctrl, shift, alt) produce control
				// characters (rune < 32) that must NOT delete the selection.
				isContentKey := s == "backspace" || s == "delete" || s == "enter" || s == "tab" ||
					(len([]rune(s)) == 1 && []rune(s)[0] >= ' ')
				if m.hasSelection() && isContentKey {
					m.deleteSelectionInPlace()
					if s == "backspace" || s == "delete" {
						skipTextarea = true
					}
				}
				if !skipTextarea {
					m.textarea, taCmd = m.textarea.Update(msg)
				}
				if isContentKey {
					action := ActionInsert
					if s == "backspace" || s == "delete" {
						action = ActionDelete
					} else if s == "enter" || s == "tab" || s == " " {
						action = ActionOther
					}
					m.trackChange(prevVal, prevLine, prevCol, action)
				}
			} else {
				// Only explicit navigation keys clear selection
				clearsSelection := s == "up" || s == "down" || s == "left" || s == "right" ||
					s == "ctrl+left" || s == "ctrl+right" ||
					s == "pgup" || s == "pgdown" || s == "home" || s == "end" ||
					s == "esc"
				if clearsSelection {
					m.clearSelection()
				}
				m.lastAction = ActionNone
				m.actionCount = 0
				m.textarea, taCmd = m.textarea.Update(msg)
			}
		} else {
			// Not a KeyMsg (e.g., Blink, WindowSizeMsg)
			m.textarea, taCmd = m.textarea.Update(msg)
		}
		cmds = append(cmds, taCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateViewport() {
	content := m.textarea.Value()
	if content != m.lastHighlightedValue {
		// Ensure we highlight with normalized newlines if chroma needs it, 
		// but highlighter.go already splits by \n
		highlighted, _ := editor.Highlight(content, m.filename)
		m.highlightedContent = highlighted
		m.lastHighlightedValue = content
	}
	
	var final string
	if m.mode == ModeSearchNav {
		final = editor.HighlightSearch(m.highlightedContent, m.searchQuery, m.matchIndex)
	} else if m.hasSelection() {
		start, end := m.selectionRange()
		final = editor.HighlightSelection(m.highlightedContent, start.Line, start.Col, end.Line, end.Col)
	} else {
		final = editor.HighlightCursor(m.highlightedContent, m.textarea.Line(), m.textarea.LineInfo().CharOffset)
	}
	
	numWidth := 0
	if m.showLineNumbers {
		numWidth = len(fmt.Sprintf("%d", m.textarea.MaxHeight)) + 3
	}
	contentWidth := m.width - numWidth

	// Update xOffset based on cursor position
	cursorCol := m.textarea.LineInfo().CharOffset
	if cursorCol < m.xOffset {
		m.xOffset = cursorCol
	} else if cursorCol >= m.xOffset+contentWidth {
		m.xOffset = cursorCol - contentWidth + 1
	}

	if m.showLineNumbers {
		var sb strings.Builder
		lines := strings.Split(final, "\n")
		numOnlyWidth := numWidth - 3

		for i, line := range lines {
			if i == len(lines)-1 && line == "" {
				break
			}
			lineNumberStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D4AF37"))
			if i == m.textarea.Line() {
				lineNumberStyle = lineNumberStyle.Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
			}
			sb.WriteString(lineNumberStyle.Render(fmt.Sprintf(" %*d ", numOnlyWidth, i+1)))
			
			displayLine := line
			if contentWidth > 0 {
				displayLine = sliceAnsi(line, m.xOffset, contentWidth)
			}
			sb.WriteString(displayLine)

			if i < len(lines)-1 {
				sb.WriteByte('\n')
			}
		}
		final = sb.String()
	} else {
		lines := strings.Split(final, "\n")
		for i, line := range lines {
			lines[i] = sliceAnsi(line, m.xOffset, m.width)
		}
		final = strings.Join(lines, "\n")
	}
	
	m.viewport.SetContent(final)
	
	if m.mode == ModeSearchNav && len(m.matches) > 0 && m.matchIndex >= 0 {
		offset := m.matches[m.matchIndex]
		lineNum := strings.Count(content[:offset], "\n")
		m.viewport.SetYOffset(lineNum - m.viewport.Height/2)
	} else {
		lineNum := m.textarea.Line()
		if lineNum < m.viewport.YOffset {
			m.viewport.SetYOffset(lineNum)
		} else if lineNum >= m.viewport.YOffset+m.viewport.Height {
			m.viewport.SetYOffset(lineNum - m.viewport.Height + 1)
		}
	}
}

func (m *Model) undo() {
	if len(m.undoStack) == 0 {
		return
	}

	m.redoStack = append(m.redoStack, UndoState{
		Content: m.textarea.Value(),
		Line:    m.textarea.Line(),
		Col:     m.textarea.LineInfo().CharOffset,
	})
	prev := m.undoStack[len(m.undoStack)-1]
	m.undoStack = m.undoStack[:len(m.undoStack)-1]

	m.textarea.SetValue(prev.Content)
	m.setCursorPos(prev.Line, prev.Col)
	m.modified = true
	if m.textarea.Value() == m.initialContent {
		m.modified = false
	}
	m.lastAction = ActionNone
	m.actionCount = 0
}

func (m *Model) redo() {
	if len(m.redoStack) == 0 {
		return
	}

	m.undoStack = append(m.undoStack, UndoState{
		Content: m.textarea.Value(),
		Line:    m.textarea.Line(),
		Col:     m.textarea.LineInfo().CharOffset,
	})
	next := m.redoStack[len(m.redoStack)-1]
	m.redoStack = m.redoStack[:len(m.redoStack)-1]

	m.textarea.SetValue(next.Content)
	m.setCursorPos(next.Line, next.Col)
	m.modified = true
	if m.textarea.Value() == m.initialContent {
		m.modified = false
	}
	m.lastAction = ActionNone
	m.actionCount = 0
}

// Selection helpers

func (m *Model) cursorPos() Pos {
	return Pos{m.textarea.Line(), m.textarea.LineInfo().CharOffset}
}

func (m *Model) hasSelection() bool {
	return m.selecting && m.selAnchor.Line >= 0
}

func (m *Model) startOrExtendSelection() {
	if !m.selecting {
		m.selAnchor = m.cursorPos()
		m.selecting = true
	}
}

func (m *Model) clearSelection() {
	m.selecting = false
	m.selAnchor = Pos{-1, -1}
}

func (m *Model) selectionRange() (Pos, Pos) {
	anchor := m.selAnchor
	cursor := m.cursorPos()
	if anchor.Line < cursor.Line || (anchor.Line == cursor.Line && anchor.Col < cursor.Col) {
		return anchor, cursor
	}
	return cursor, anchor
}

func (m *Model) getSelectedText() string {
	if !m.hasSelection() {
		return ""
	}
	start, end := m.selectionRange()
	lines := strings.Split(m.textarea.Value(), "\n")

	if start.Line == end.Line {
		r := []rune(lines[start.Line])
		endCol := min(end.Col, len(r))
		startCol := min(start.Col, len(r))
		return string(r[startCol:endCol])
	}

	var sb strings.Builder
	r := []rune(lines[start.Line])
	sb.WriteString(string(r[min(start.Col, len(r)):]))
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(lines[i])
	}
	sb.WriteByte('\n')
	r = []rune(lines[end.Line])
	sb.WriteString(string(r[:min(end.Col, len(r))]))

	return sb.String()
}

func (m *Model) deleteSelectionInPlace() {
	if !m.hasSelection() {
		return
	}
	start, end := m.selectionRange()
	lines := strings.Split(m.textarea.Value(), "\n")

	var sb strings.Builder
	for i := 0; i < start.Line; i++ {
		sb.WriteString(lines[i])
		sb.WriteByte('\n')
	}
	startRunes := []rune(lines[start.Line])
	startCol := min(start.Col, len(startRunes))
	sb.WriteString(string(startRunes[:startCol]))

	endRunes := []rune(lines[end.Line])
	endCol := min(end.Col, len(endRunes))
	sb.WriteString(string(endRunes[endCol:]))

	for i := end.Line + 1; i < len(lines); i++ {
		sb.WriteByte('\n')
		sb.WriteString(lines[i])
	}

	m.textarea.SetValue(sb.String())
	m.setCursorPos(start.Line, start.Col)
	m.clearSelection()
}

func (m *Model) setCursorPos(line, col int) {
	for m.textarea.Line() > line {
		m.textarea.CursorUp()
	}
	for m.textarea.Line() < line {
		m.textarea.CursorDown()
	}
	m.textarea.SetCursor(col)
}

// Cursor movement helpers (textarea doesn't export these)

func (m *Model) moveCursorLeft() {
	col := m.textarea.LineInfo().CharOffset
	if col > 0 {
		m.textarea.SetCursor(col - 1)
	} else if m.textarea.Line() > 0 {
		m.textarea.CursorUp()
		m.textarea.CursorEnd()
	}
}

func (m *Model) moveCursorRight() {
	lines := strings.Split(m.textarea.Value(), "\n")
	col := m.textarea.LineInfo().CharOffset
	lineLen := len([]rune(lines[m.textarea.Line()]))
	if col < lineLen {
		m.textarea.SetCursor(col + 1)
	} else if m.textarea.Line() < len(lines)-1 {
		m.textarea.CursorDown()
		m.textarea.CursorStart()
	}
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

func (m *Model) wordLeft() {
	lines := strings.Split(m.textarea.Value(), "\n")
	line := m.textarea.Line()
	col := m.textarea.LineInfo().CharOffset

	if col == 0 {
		if line > 0 {
			m.textarea.CursorUp()
			m.textarea.CursorEnd()
		}
		return
	}

	runes := []rune(lines[line])
	pos := col - 1
	for pos > 0 && !isWordChar(runes[pos]) {
		pos--
	}
	for pos > 0 && isWordChar(runes[pos-1]) {
		pos--
	}
	m.textarea.SetCursor(pos)
}

func (m *Model) wordRight() {
	lines := strings.Split(m.textarea.Value(), "\n")
	line := m.textarea.Line()
	col := m.textarea.LineInfo().CharOffset
	runes := []rune(lines[line])

	if col >= len(runes) {
		if line < len(lines)-1 {
			m.textarea.CursorDown()
			m.textarea.CursorStart()
		}
		return
	}

	pos := col
	for pos < len(runes) && isWordChar(runes[pos]) {
		pos++
	}
	for pos < len(runes) && !isWordChar(runes[pos]) {
		pos++
	}
	m.textarea.SetCursor(pos)
}

func (m *Model) trackChange(prevVal string, prevLine, prevCol int, currentAction ActionType) {
	newVal := m.textarea.Value()
	if newVal != prevVal {
		m.modified = true
		
		// Group continuous typing/deleting actions
		shouldPush := true
		if m.lastAction == currentAction {
			if currentAction == ActionInsert || currentAction == ActionDelete {
				m.actionCount++
				if m.actionCount < 20 {
					shouldPush = false
				} else {
					m.actionCount = 0
				}
			}
		} else {
			m.actionCount = 0
		}

		if shouldPush {
			m.undoStack = append(m.undoStack, UndoState{
				Content: prevVal,
				Line:    prevLine,
				Col:     prevCol,
			})
			m.redoStack = nil
			if len(m.undoStack) > 100 {
				m.undoStack = m.undoStack[1:]
			}
		}
		
		m.lastAction = currentAction
	}
}

func (m *Model) saveFile() {
	content := m.textarea.Value()
	if m.lineEnding == "\r\n" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	_ = os.WriteFile(m.filename, []byte(content), 0644)
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

func (m *Model) View() string {
	var body string
	if m.mode == ModeSearchNav || m.mode == ModeEdit || m.mode == ModeSearchInput || m.mode == ModeQuitConfirm {
		m.updateViewport()
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

func (m *Model) headerView() string {
	if m.headerCache != "" && m.width == m.lastWidth && m.mode == m.lastMode && m.modified == m.lastModified {
		return m.headerCache
	}

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
	
	leLabel := " LF "
	if m.lineEnding == "\r\n" {
		leLabel = " CRLF "
	}
	
	title := titleStyle.Render("ATLAS ED")
	mLabel := modeStyle.Render(currentMode)
	status := statusStyle.Render(" " + m.filename + modChar + " ")
	leStatus := statusStyle.Render(leLabel)
	
	line := strings.Repeat("─", max(0, m.width-lipgloss.Width(title)-lipgloss.Width(mLabel)-lipgloss.Width(status)-lipgloss.Width(leStatus)))
	m.headerCache = lipgloss.JoinHorizontal(lipgloss.Center, title, mLabel, status, leStatus, infoStyle.Render(line))
	m.lastWidth = m.width
	m.lastMode = m.mode
	m.lastModified = m.modified

	return m.headerCache
}

func (m *Model) footerView() string {
	if m.mode == ModeSearchInput {
		return m.searchInput.View()
	}

	cursorLine := m.textarea.Line() + 1
	cursorCol := m.textarea.LineInfo().CharOffset + 1

	if m.footerCache != "" && m.width == m.lastWidth && m.mode == m.lastMode && 
		len(m.matches) == m.lastMatchesCount && m.matchIndex == m.lastMatchIndex &&
		m.lastLine == cursorLine && m.lastCol == cursorCol {
		return m.footerCache
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
			helpKeyStyle.Render(" ^Z "), helpDescStyle.Render("undo "),
			helpKeyStyle.Render(" ^Y "), helpDescStyle.Render("redo "),
			helpKeyStyle.Render(" ^F "), helpDescStyle.Render("find "),
			helpKeyStyle.Render(" ^C "), helpDescStyle.Render("copy "),
			helpKeyStyle.Render(" ^V "), helpDescStyle.Render("paste "),
			helpKeyStyle.Render(" ^Q "), helpDescStyle.Render("quit "),
		)
	}
	
	matchInfo := ""
	if len(m.matches) > 0 {
		matchInfo = matchCountStyle.Render(fmt.Sprintf(" MATCH %d/%d ", m.matchIndex+1, len(m.matches)))
	}

	cursorInfo := statusStyle.Render(fmt.Sprintf(" LN %d, COL %d ", cursorLine, cursorCol))

	gap := max(0, m.width-lipgloss.Width(help)-lipgloss.Width(matchInfo)-lipgloss.Width(cursorInfo)-2)
	line := strings.Repeat(" ", gap)
	
	m.footerCache = lipgloss.JoinHorizontal(lipgloss.Center, help, line, matchInfo, cursorInfo)
	m.lastMatchesCount = len(m.matches)
	m.lastMatchIndex = m.matchIndex
	m.lastLine = cursorLine
	m.lastCol = cursorCol

	return m.footerCache
}

func max(a, b int) int {
	if a > b { return a }
	return b
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

func truncateAnsi(s string, limit int) string {
	return sliceAnsi(s, 0, limit)
}

func sliceAnsi(s string, start, width int) string {
	if width <= 0 {
		return ""
	}

	var result strings.Builder
	var activeAnsi strings.Builder
	runeCount := 0
	byteIdx := 0

	for byteIdx < len(s) {
		if strings.HasPrefix(s[byteIdx:], "\x1b[") {
			end := strings.IndexAny(s[byteIdx:], "mABCDHJKfhnpsu")
			if end == -1 {
				if runeCount >= start && runeCount < start+width {
					result.WriteString(s[byteIdx:])
				}
				break
			}
			ansi := s[byteIdx : byteIdx+end+1]
			if runeCount < start {
				activeAnsi.WriteString(ansi)
			} else if runeCount < start+width {
				result.WriteString(ansi)
			}
			byteIdx += end + 1
		} else {
			r, size := nextRune(s[byteIdx:])
			if runeCount == start {
				result.WriteString(activeAnsi.String())
			}
			if runeCount >= start && runeCount < start+width {
				result.WriteRune(r)
			}
			byteIdx += size
			runeCount++
			if runeCount >= start+width {
				result.WriteString("\x1b[0m") // Reset at the end of slice
				break
			}
		}
	}
	return result.String()
}

func nextRune(s string) (rune, int) {
	if len(s) == 0 {
		return 0, 0
	}
	for i, r := range s {
		if i == 0 {
			return r, len(string(r))
		}
	}
	return 0, 0
}
