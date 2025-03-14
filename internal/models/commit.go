package models

// CommitOutput represents the structure of a git commit with its details
type CommitOutput struct {
	CommitID     string `json:"commit_id"`
	Message      string `json:"message"`
	Files        []File `json:"files"`
	NeedsRewrite bool   `json:"needs_rewrite"`
}

// File represents a single file change in a commit
type File struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// NewCommitMessage represents the structure of a rewritten commit message
type NewCommitMessage struct {
	CommitID string              `json:"commit_id"`
	Messages []map[string]string `json:"messages"`
}

// RewriteOutput represents an entry in the dry run output file
type RewriteOutput struct {
	CommitID     string `json:"commit_id"`
	OriginalMsg  string `json:"original_message"`
	RewrittenMsg string `json:"rewritten_message"`
	FilesChanged int    `json:"files_changed"`
	IsApplied    bool   `json:"is_applied"`
}

// OllamaOutputFormat defines the JSON schema for Ollama API responses
type OllamaOutputFormat struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required"`
}
