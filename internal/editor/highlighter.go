package editor

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	searchMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#D4AF37")).
				Foreground(lipgloss.Color("#000000"))

	currentMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#FFFFFF")).
				Foreground(lipgloss.Color("#000000")).
				Bold(true)

	selectionStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#264F78")).
				Foreground(lipgloss.Color("#FFFFFF"))
)

func Highlight(content, filename string) (string, error) {
	lexer := lexers.Get(filename)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("monokai")
	formatter := formatters.Get("terminal256")

	iterator, _ := lexer.Tokenise(nil, content)
	var buf bytes.Buffer
	formatter.Format(&buf, style, iterator)

	return buf.String(), nil
}

func HighlightSearch(highlighted, query string, targetIdx int) string {
	if query == "" {
		return highlighted
	}
	
	lowerQuery := strings.ToLower(query)
	var result strings.Builder
	cursor := 0
	matchCounter := 0
	
	// Pre-size builder to avoid re-allocations
	result.Grow(len(highlighted) + 100)

	for cursor < len(highlighted) {
		start := strings.Index(highlighted[cursor:], "\x1b[")
		if start == -1 {
			res, count := highlightPlainPart(highlighted[cursor:], lowerQuery, targetIdx, matchCounter)
			result.WriteString(res)
			matchCounter = count
			break
		}
		
		start += cursor
		if start > cursor {
			res, count := highlightPlainPart(highlighted[cursor:start], lowerQuery, targetIdx, matchCounter)
			result.WriteString(res)
			matchCounter = count
		}
		
		end := strings.IndexAny(highlighted[start:], "mABCDHJKfhnpsu")
		if end == -1 {
			result.WriteString(highlighted[start:])
			break
		}
		end += start + 1
		result.WriteString(highlighted[start:end])
		cursor = end
	}
	
	return result.String()
}

func highlightPlainPart(text, lowerQuery string, targetIdx, currentCount int) (string, int) {
	if lowerQuery == "" || text == "" {
		return text, currentCount
	}

	lowerText := strings.ToLower(text)
	var result strings.Builder
	cursor := 0
	count := currentCount
	
	for {
		idx := strings.Index(lowerText[cursor:], lowerQuery)
		if idx == -1 {
			result.WriteString(text[cursor:])
			break
		}

		idx += cursor
		result.WriteString(text[cursor:idx])

		matchText := text[idx : idx+len(lowerQuery)]
		style := searchMatchStyle
		if count == targetIdx {
			style = currentMatchStyle
		}
		result.WriteString(style.Render(matchText))

		count++
		cursor = idx + len(lowerQuery)
	}
	return result.String(), count
}

func HighlightSelection(highlighted string, startLine, startCol, endLine, endCol int) string {
	lines := strings.Split(highlighted, "\n")

	for lineIdx := startLine; lineIdx <= endLine && lineIdx < len(lines); lineIdx++ {
		fromCol := 0
		toCol := -1 // -1 means end of line

		if lineIdx == startLine {
			fromCol = startCol
		}
		if lineIdx == endLine {
			toCol = endCol
		}

		lines[lineIdx] = highlightRange(lines[lineIdx], fromCol, toCol)
	}

	return strings.Join(lines, "\n")
}

func highlightRange(line string, fromCol, toCol int) string {
	var before, selected, after strings.Builder
	byteIdx := 0
	runeIdx := 0

	for byteIdx < len(line) {
		if strings.HasPrefix(line[byteIdx:], "\x1b[") {
			end := strings.IndexAny(line[byteIdx:], "mABCDHJKfhnpsu")
			if end == -1 {
				if runeIdx < fromCol {
					before.WriteString(line[byteIdx:])
				} else if toCol != -1 && runeIdx >= toCol {
					after.WriteString(line[byteIdx:])
				}
				break
			}
			ansi := line[byteIdx : byteIdx+end+1]
			if runeIdx < fromCol {
				before.WriteString(ansi)
			} else if toCol != -1 && runeIdx >= toCol {
				after.WriteString(ansi)
			}
			// Skip ANSI codes inside selected region
			byteIdx += end + 1
		} else {
			r, size := nextRune(line[byteIdx:])
			if runeIdx < fromCol {
				before.WriteRune(r)
			} else if toCol == -1 || runeIdx < toCol {
				selected.WriteRune(r)
			} else {
				after.WriteRune(r)
			}
			byteIdx += size
			runeIdx++
		}
	}

	result := before.String()
	if selected.Len() > 0 {
		result += selectionStyle.Render(selected.String())
	}
	result += after.String()
	return result
}

func HighlightCursor(highlighted string, cursorLine, cursorCol int) string {
	return highlightCursorInLine(highlighted, cursorLine, cursorCol)
}

func highlightCursorInLine(highlighted string, cursorLine, cursorCol int) string {
	lines := strings.Split(highlighted, "\n")
	if cursorLine < 0 || cursorLine >= len(lines) {
		return highlighted
	}

	line := lines[cursorLine]
	var result strings.Builder
	
	// We want to find the cursorCol-th rune, skipping ANSI codes.
	
	byteIdx := 0
	currentRuneIdx := 0
	inserted := false
	
	for byteIdx < len(line) {
		if !inserted && currentRuneIdx == cursorCol {
			// We found the position!
			// Check if we're at an ANSI escape
			if strings.HasPrefix(line[byteIdx:], "\x1b[") {
				// Skip the ANSI escape first so we don't break it
				end := strings.IndexAny(line[byteIdx:], "mABCDHJKfhnpsu")
				if end != -1 {
					result.WriteString(line[byteIdx : byteIdx+end+1])
					byteIdx += end + 1
					continue
				}
			}
			
			// Render cursor
			r, size := nextRune(line[byteIdx:])
			char := string(r)
			if size == 0 { char = " " } // End of line
			
			result.WriteString(lipgloss.NewStyle().Background(lipgloss.Color("#FFFFFF")).Foreground(lipgloss.Color("#000000")).Render(char))
			
			if size > 0 {
				byteIdx += size
				currentRuneIdx++
			}
			inserted = true
			continue
		}
		
		if strings.HasPrefix(line[byteIdx:], "\x1b[") {
			end := strings.IndexAny(line[byteIdx:], "mABCDHJKfhnpsu")
			if end == -1 {
				result.WriteString(line[byteIdx:])
				break
			}
			result.WriteString(line[byteIdx : byteIdx+end+1])
			byteIdx += end + 1
		} else {
			r, size := nextRune(line[byteIdx:])
			result.WriteRune(r)
			byteIdx += size
			currentRuneIdx++
		}
	}
	
	if !inserted {
		// Cursor at the very end of the line
		result.WriteString(lipgloss.NewStyle().Background(lipgloss.Color("#FFFFFF")).Foreground(lipgloss.Color("#000000")).Render(" "))
	}
	
	lines[cursorLine] = result.String()
	return strings.Join(lines, "\n")
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
