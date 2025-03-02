package helpers

import (
	"strings"
)

// TruncateString truncates a string to the specified length and adds an ellipsis if needed
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// SanitizeCommitMessage removes any unwanted characters from a commit message
func SanitizeCommitMessage(message string) string {
	// Remove leading/trailing whitespace
	message = strings.TrimSpace(message)
	// Replace multiple newlines with a single newline
	message = strings.ReplaceAll(message, "\n\n\n", "\n\n")
	return message
}
