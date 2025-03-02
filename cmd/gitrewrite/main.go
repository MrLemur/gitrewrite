package main

import (
	"fmt"
	"os"

	"github.com/MrLemur/gitrewrite/internal/commands"
	"github.com/MrLemur/gitrewrite/internal/ui"
)

func main() {
	// Setup TUI
	ui.SetupTUI()
	go func() {
		if err := ui.App.SetRoot(ui.MainFlex, true).Run(); err != nil {
			panic(err)
		}
	}()

	ui.LogInfo("Git Commit Message Rewriter started")
	ui.LogInfo("Keyboard controls:")
	ui.LogInfo("  Ctrl+C: Exit program")
	ui.LogInfo("  PgUp/PgDn: Scroll log up/down")
	ui.LogInfo("  Home/End: Jump to start/end of log")

	// Parse command line flags
	commands.ParseFlags()

	// Validate repository path
	if commands.RepoPath == "" {
		fmt.Println("Please provide a path to a git repository using -repo=/path/to/repo")
		os.Exit(1)
	}

	// Run the application
	commands.RunApplication()
}
