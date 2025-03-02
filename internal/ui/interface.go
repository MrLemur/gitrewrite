package ui

// TextView defines an interface for text view components
// This allows for easier mocking in tests
type TextView interface {
	Clear()
	SetText(text string)
	GetText(bool) string
}
