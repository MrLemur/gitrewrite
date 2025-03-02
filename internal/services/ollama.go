package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MrLemur/gitrewrite/internal/models"
	"github.com/MrLemur/gitrewrite/internal/ui"
	ollama "github.com/ollama/ollama/api"
)

// SendOllamaMessage sends a request to the Ollama API
func SendOllamaMessage(model string, messages []ollama.Message, format json.RawMessage, temperature float64) (string, error) {
	client, err := ollama.ClientFromEnvironment()
	if model == "" {
		return "", fmt.Errorf("Ollama model must be specified")
	}
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	var response string
	respFunc := func(resp ollama.ChatResponse) error {
		response += resp.Message.Content
		return nil
	}
	err = client.Chat(
		ctx,
		&ollama.ChatRequest{Model: model, Messages: messages, Format: format, Options: map[string]any{"temperature": temperature}},
		respFunc,
	)
	if err != nil {
		return "", err
	}
	return response, nil
}

// GenerateNewCommitMessage generates a new commit message using Ollama
func GenerateNewCommitMessage(commit models.CommitOutput, model string, temperature float64) (models.NewCommitMessage, error) {
	ui.UpdateStatus("Generating new commit message...")
	messages := []ollama.Message{
		{
			Role: "system",
			Content: "Act as a senior engineer enforcing Conventional Commits. Input: Commit data with ID/message/diffs. Output: JSON with commit_id and messages array. Each message object will contain the field type, desciption and affected app. Rules:\n" +
				"1. Types: feat, fix, chore, docs, refactor, perf\n" +
				"2. Max 100 characters\n" +
				"3. Explain what changed + why\n" +
				"4. One message per logical change\n" +
				"5. Group related files under one message\n" +
				"6. Never use markdown/symbols\n" +
				"7. Distill affect app name from the file path." +
				"8: Example: {'type':'chore','description':'upgrade Docker image to v21.3.1','affected_app':'hortusfox'}",
		},
		{Role: "user", Content: "Generate a new commit message for the following commit:"},
	}
	format := models.OllamaOutputFormat{
		Type: "object",
		Properties: map[string]interface{}{
			"commit_id": map[string]interface{}{"type": "string"},
			"messages": map[string]interface{}{
				"type": "array",
				"properties": map[string]interface{}{
					"type":         map[string]interface{}{"type": "string"},
					"description":  map[string]interface{}{"type": "string"},
					"affected_app": map[string]interface{}{"type": "string"},
				},
			},
		},
		Required: []string{"commit_id", "messages"},
	}

	// Process files to handle large diffs
	var processedFiles []models.File
	for _, file := range commit.Files {
		var processedFile models.File
		if len(file.Diff) > 2048 {
			processedFile = models.File{Diff: file.Diff[:2048], Path: file.Path}
		} else {
			processedFile = file
		}
		processedFiles = append(processedFiles, processedFile)
	}
	commit.Files = processedFiles

	var newCommit models.NewCommitMessage

	commitJSON, _ := json.Marshal(commit)
	formatJSON, _ := json.Marshal(format)
	formatRaw := json.RawMessage(formatJSON)

	messages = append(messages, ollama.Message{Role: "user", Content: string(commitJSON)})

	ui.LogInfo("Sending commit %s to Ollama for processing", commit.CommitID[:8])
	resp, err := SendOllamaMessage(model, messages, formatRaw, temperature)
	if err != nil {
		ui.LogError("Failed to send Ollama message: %v", err)
		return models.NewCommitMessage{}, fmt.Errorf("Failed to send Ollama message: %v", err)
	}

	err = json.Unmarshal([]byte(resp), &newCommit)
	if err != nil {
		ui.LogError("Failed to unmarshal Ollama response: %v", err)
		return models.NewCommitMessage{}, fmt.Errorf("Failed to unmarshal Ollama response: %v", err)
	}

	ui.UpdateStatus("Ready")
	return newCommit, nil
}
