package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	// Timing variables for ETA calculation
	StartTime           time.Time
	LastCommitStartTime time.Time
	TotalProcessingTime time.Duration
	CommitTimings       []time.Duration
	// Debug logging variables
	debugLogger    *os.File
	debugLogMutex  sync.Mutex
	isDebugLogging bool
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

// InitDebugLogging sets up debug logging to a file if a path is provided
func InitDebugLogging(logFilePath string) error {
	if logFilePath == "" {
		return nil // Debug logging not enabled
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(logFilePath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create debug log directory: %v", err)
		}
	}

	// Open log file for writing (create if not exists, append if exists)
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug log file: %v", err)
	}

	debugLogger = file
	isDebugLogging = true

	// Log start of session
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(debugLogger, "\n\n===== DEBUG LOG SESSION STARTED AT %s =====\n\n", timestamp)

	return nil
}

// CloseDebugLog closes the debug log file if it's open
func CloseDebugLog() {
	if debugLogger != nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintf(debugLogger, "\n\n===== DEBUG LOG SESSION ENDED AT %s =====\n\n", timestamp)
		debugLogger.Close()
		debugLogger = nil
		isDebugLogging = false
	}
}

// LogShellCommand logs shell commands to the debug log file
func LogShellCommand(command string, args []string, workDir string) {
	if !isDebugLogging {
		return
	}

	debugLogMutex.Lock()
	defer debugLogMutex.Unlock()

	fullTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
	cmdLine := fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	fmt.Fprintf(debugLogger, "[%s] SHELL CMD: [dir=%s] %s\n", fullTimestamp, workDir, cmdLine)
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
	completedWidth := int(float64(barWidth) * percentage / 100)

	// Calculate ETA
	var etaText string
	if ProcessedCommits > 0 {
		// Calculate average time per commit
		var avgTimePerCommit time.Duration

		// Only use timing data if we have any
		if len(CommitTimings) > 0 {
			// Use median of last few commits for more stable estimates
			recentTimings := append([]time.Duration{}, CommitTimings...)
			sort.Slice(recentTimings, func(i, j int) bool {
				return recentTimings[i] < recentTimings[j]
			})
			medianIdx := len(recentTimings) / 2
			avgTimePerCommit = recentTimings[medianIdx]
		} else {
			// Fall back to simple average if we don't have enough samples
			if TotalProcessingTime > 0 && ProcessedCommits > 0 {
				avgTimePerCommit = TotalProcessingTime / time.Duration(ProcessedCommits)
			} else {
				// Default to 5 seconds if we don't have data yet
				avgTimePerCommit = 5 * time.Second
			}
		}

		// Ensure we don't have a zero duration (minimum 500ms per commit)
		if avgTimePerCommit < 500*time.Millisecond {
			avgTimePerCommit = 500 * time.Millisecond
		}

		// Calculate remaining time
		remainingCommits := TotalCommits - ProcessedCommits
		remainingTime := avgTimePerCommit * time.Duration(remainingCommits)

		// Format ETA nicely
		etaText = fmt.Sprintf(" ETA: %s", formatDuration(remainingTime))
	} else {
		etaText = " ETA: calculating..."
	}

	progressText := fmt.Sprintf("[green]%d/%d commits processed (%.1f%%)[white]%s",
		ProcessedCommits, TotalCommits, percentage, etaText)
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

	if isDebugLogging {
		debugLogMutex.Lock()
		defer debugLogMutex.Unlock()
		fullTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(debugLogger, "[%s] INFO: %s\n", fullTimestamp, msg)
	}
}

// LogError logs an error message
func LogError(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(LogView, "[blue]%s[white] [red]ERROR[white]: %s\n", timestamp, msg)

	if isDebugLogging {
		debugLogMutex.Lock()
		defer debugLogMutex.Unlock()
		fullTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(debugLogger, "[%s] ERROR: %s\n", fullTimestamp, msg)
	}
}

// LogWarning logs a warning message
func LogWarning(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(LogView, "[blue]%s[white] [yellow]WARNING[white]: %s\n", timestamp, msg)

	if isDebugLogging {
		debugLogMutex.Lock()
		defer debugLogMutex.Unlock()
		fullTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(debugLogger, "[%s] WARNING: %s\n", fullTimestamp, msg)
	}
}

// LogSuccess logs a success message
func LogSuccess(format string, args ...interface{}) {
	timestamp := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(LogView, "[blue]%s[white] [green]SUCCESS[white]: %s\n", timestamp, msg)

	if isDebugLogging {
		debugLogMutex.Lock()
		defer debugLogMutex.Unlock()
		fullTimestamp := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(debugLogger, "[%s] SUCCESS: %s\n", fullTimestamp, msg)
	}
}

// UpdateCommitDetails updates the details of the current commit being processed
func UpdateCommitDetails(id string, totalFiles int, diffSize int, old, new string) {
	CommitDetails.Clear()
	fmt.Fprintf(CommitDetails, "[yellow]Commit ID:[white]\n%s\n\n", id)
	fmt.Fprintf(CommitDetails, "[red]Total Files Changed:[white]\n%d\n", totalFiles)

	// Format diff size nicely
	if diffSize >= 0 {
		if diffSize >= 1024 {
			fmt.Fprintf(CommitDetails, "[red]Total Diff Size:[white]\n%.2f KB\n\n", float64(diffSize)/1024)
		} else {
			fmt.Fprintf(CommitDetails, "[red]Total Diff Size:[white]\n%d bytes\n\n", diffSize)
		}
	}

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

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	if d < time.Minute {
		return fmt.Sprintf("%ds", d.Seconds())
	}

	if d < time.Hour {
		m := d / time.Minute
		s := (d % time.Minute) / time.Second
		return fmt.Sprintf("%dm %ds", m, s)
	}

	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second

	return fmt.Sprintf("%dh %dm %ds", h, m, s)
}
