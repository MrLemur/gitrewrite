package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TUI components
var (
	App                *tview.Application
	MainFlex           *tview.Flex
	ProgressBar        *tview.TextView
	LogView            *tview.TextView
	StatusBar          *tview.TextView
	CommitDetails      *tview.TextView
	LastCommitDetails  *tview.TextView
	TotalCommits       int
	ProcessedCommits   int
	ConfirmationResult bool
	ConfirmationDone   bool
)

// SetupTUI initializes the terminal UI components
func SetupTUI() {
	App = tview.NewApplication()
	MainFlex = tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("GitRewrite").
		SetTextColor(tcell.ColorYellow)

	ProgressBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	// Configure log view with auto-scrolling
	LogView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true).
		SetChangedFunc(func() {
			// Auto-scroll to the bottom when new content is added
			App.QueueUpdateDraw(func() {
				LogView.ScrollToEnd()
			})
		})
	LogView.SetBorder(true)
	LogView.SetTitle("Log")
	LogView.SetTitleColor(tcell.ColorGreen)

	CommitDetails = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true).
		SetChangedFunc(func() {
			App.Draw()
		})
	CommitDetails.SetBorder(true)
	CommitDetails.SetTitle("Current Commit")
	CommitDetails.SetTitleColor(tcell.ColorBlue)

	LastCommitDetails = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true).
		SetChangedFunc(func() {
			App.Draw()
		})
	LastCommitDetails.SetBorder(true)
	LastCommitDetails.SetTitle("Last Processed Commit")
	LastCommitDetails.SetTitleColor(tcell.ColorPurple)

	StatusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("[yellow]Press Ctrl+C to exit[white]")

	// Create a flex container for commit details
	commitDetailsFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(CommitDetails, 0, 1, false).
		AddItem(LastCommitDetails, 0, 1, false)

	MainFlex.AddItem(header, 1, 1, false).
		AddItem(ProgressBar, 1, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(LogView, 0, 3, false).
			AddItem(commitDetailsFlex, 0, 2, false),
			0, 10, false).
		AddItem(StatusBar, 1, 1, false)

	// Add keyboard controls for scrolling logs
	App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			App.Stop()
			os.Exit(0)
			return nil
		}
		if event.Key() == tcell.KeyPgUp {
			_, _, _, height := LogView.GetInnerRect()
			row, _ := LogView.GetScrollOffset()
			LogView.ScrollTo(row-height+1, 0)
			return nil
		} else if event.Key() == tcell.KeyPgDn {
			_, _, _, height := LogView.GetInnerRect()
			row, _ := LogView.GetScrollOffset()
			LogView.ScrollTo(row+height-1, 0)
			return nil
		} else if event.Key() == tcell.KeyEnd {
			LogView.ScrollToEnd()
			return nil
		} else if event.Key() == tcell.KeyHome {
			LogView.ScrollTo(0, 0)
			return nil
		}
		return event
	})
}

// ShowConfirmationDialog displays a confirmation dialog and waits for user input
func ShowConfirmationDialog(message string) bool {
	// Reset confirmation variables
	ConfirmationResult = false
	ConfirmationDone = false

	// Create the modal dialog
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"Yes", "No"}).
		SetFocus(1). // Set focus on the second button ("No")
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			ConfirmationResult = (buttonLabel == "Yes")
			ConfirmationDone = true
			App.SetRoot(MainFlex, true)
		}).
		SetBackgroundColor(tcell.ColorDefault).
		SetTextColor(tcell.ColorRed)

	// Show the modal dialog
	App.SetRoot(modal, true)
	App.Draw()

	// Wait for the user's response
	for !ConfirmationDone {
		time.Sleep(100 * time.Millisecond)
	}

	return ConfirmationResult
}

// UpdateProgressBar updates the progress bar with the current status
func UpdateProgressBar() {
	if TotalCommits == 0 {
		ProgressBar.SetText("[yellow]No commits to process[white]")
		return
	}
	percentage := float64(ProcessedCommits) / float64(TotalCommits) * 100
	barWidth := 50
	completedWidth := int(float64(barWidth) * float64(ProcessedCommits) / float64(TotalCommits))
	progressText := fmt.Sprintf("[green]%d/%d commits processed (%.1f%%)[white]",
		ProcessedCommits, TotalCommits, percentage)
	bar := ""
	for i := 0; i < barWidth; i++ {
		if i < completedWidth {
			bar += "[green]█[white]"
		} else {
			bar += "[gray]░[white]"
		}
	}
	ProgressBar.SetText(fmt.Sprintf("%s %s", bar, progressText))
	App.Draw()
}

// LogInfo logs an informational message
func LogInfo(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(LogView, "[blue]%s[white] [yellow]INFO[white]: %s\n", timestamp, msg)
}

// LogError logs an error message
func LogError(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(LogView, "[blue]%s[white] [red]ERROR[white]: %s\n", timestamp, msg)
}

// LogSuccess logs a success message
func LogSuccess(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(LogView, "[blue]%s[white] [green]SUCCESS[white]: %s\n", timestamp, msg)
}

// UpdateCommitDetails updates the details of the current commit being processed
func UpdateCommitDetails(id string, totalFiles int, old, new string) {
	CommitDetails.Clear()
	fmt.Fprintf(CommitDetails, "[yellow]Commit ID:[white]\n%s\n\n", id)
	fmt.Fprintf(CommitDetails, "[red]Total Files Changed:[white]\n%d\n\n", totalFiles)
	fmt.Fprintf(CommitDetails, "[yellow]Original Message:[white]\n%s\n\n", old)
	fmt.Fprintf(CommitDetails, "[green]New Message:[white]\n%s\n", new)
}

// MoveToLastCommit moves the current commit details to the last commit details panel
func MoveToLastCommit() {
	LastCommitDetails.Clear()
	LastCommitDetails.SetText(CommitDetails.GetText(true))
}

// UpdateStatus updates the status bar text
func UpdateStatus(text string) {
	StatusBar.SetText(fmt.Sprintf("[yellow]%s[white]", text))
	App.Draw()
}
