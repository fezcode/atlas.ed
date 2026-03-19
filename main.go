package main

import (
	"flag"
	"fmt"
	"os"

	"atlas.ed/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

var Version = "dev"

func main() {
	showVersion := flag.Bool("v", false, "Show version")
	showVersionLong := flag.Bool("version", false, "Show version")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Atlas Ed - A beautiful terminal text editor.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  atlas.ed [flags] <file>\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	
	flag.Parse()

	if *showVersion || *showVersionLong {
		fmt.Printf("atlas.ed v%s\n", Version)
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	filePath := args[0]
	content := ""
	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}
		content = string(data)
	}

	// Interactive TUI Mode
	m := ui.NewModel(filePath, content)
	p := tea.NewProgram(&m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
