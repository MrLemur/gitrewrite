package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

// CheckOllamaAvailability checks if the Ollama server is available
func CheckOllamaAvailability() error {
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama client: %v", err)
	}
	
	ctx := context.Background()
	_, err = client.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Ollama server: %v", err)
	}
	
	return nil
}

// GetModelContextSize retrieves the context window size for a model
func GetModelContextSize(model string) (int, error) {
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return 0, fmt.Errorf("failed to create Ollama client: %v", err)
	}
	
	ctx := context.Background()
	modelInfo, err := client.Show(ctx, &ollama.ShowRequest{Name: model})
	if err != nil {
		return 0, fmt.Errorf("failed to get model info from Ollama: %v", err)
	}
	
	// Context size is in modelInfo.ModelInfo under a key like "model_name.context_length"
	if modelInfo.ModelInfo == nil {
		return 0, fmt.Errorf("no model info available for %s", model)
	}
	
	// Look for the context_length key - it should be in the format "prefix.context_length"
	var contextSize int
	for key, value := range modelInfo.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			// Try to convert the value to an integer
			switch v := value.(type) {
			case float64:
				contextSize = int(v)
				ui.LogInfo("Found context size %d from key %s", contextSize, key)
				break
			case int:
				contextSize = v
				ui.LogInfo("Found context size %d from key %s", contextSize, key)
				break
			case int64:
				contextSize = int(v)
				ui.LogInfo("Found context size %d from key %s", contextSize, key)
				break
			case string:
				// Parse string to int
				if intVal, err := strconv.Atoi(v); err == nil {
					contextSize = intVal
					ui.LogInfo("Found context size %d from key %s", contextSize, key)
					break
				}
			}
		}
	}
	
	// If we couldn't extract a context size, return an error
	if contextSize == 0 {
		return 0, fmt.Errorf("could not determine context size for model %s", model)
	}
	
	return contextSize, nil
}

// EstimateTokenCount provides a rough estimate of token count for text
func EstimateTokenCount(text string) int {
	// Simple estimation: ~4 characters per token for English text
	// This is a rough approximation and varies by tokenizer
	return len(text) / 4
}

// GenerateNewCommitMessage generates a new commit message using Ollama
func GenerateNewCommitMessage(commit models.CommitOutput, model string, temperature float64, contextSize int) (models.NewCommitMessage, error) {
	ui.UpdateStatus("Generating new commit message...")
	systemPrompt := "Act as a senior engineer enforcing Conventional Commits. Input: Commit data with ID/message/diffs. Output: JSON with commit_id and messages array. Each message object will contain the field type, desciption and affected app. Rules:\n" +
		"1. Types: feat, fix, chore, docs, refactor, perf\n" +
		"2. Max 100 characters\n" +
		"3. Explain what changed + why\n" +
		"4. One message per logical change\n" +
		"5. Group related files under one message\n" +
		"6. Never use markdown/symbols\n" +
		"7. Distill affect app name from the file path." +
		"8: Example: {'type':'chore','description':'upgrade Docker image to v21.3.1','affected_app':'hortusfox'}"
	
	messages := []ollama.Message{
		{Role: "system", Content: systemPrompt},
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

	// Estimate token count
	systemTokens := EstimateTokenCount(systemPrompt)
	userPromptTokens := EstimateTokenCount("Generate a new commit message for the following commit:")
	
	// Convert commit to JSON to estimate its token count
	commitJSON, _ := json.Marshal(commit)
	commitTokens := EstimateTokenCount(string(commitJSON))
	
	// Format tokens (usually small)
	formatJSON, _ := json.Marshal(format)
	formatTokens := EstimateTokenCount(string(formatJSON))
	
	// Calculate total tokens needed for the request
	totalTokens := systemTokens + userPromptTokens + commitTokens + formatTokens
	
	// Add buffer for model's response (typically 25% of context)
	responseBuffer := contextSize / 4
	
	// Check if we'll exceed the context window
	if totalTokens + responseBuffer > contextSize {
		ui.LogError("Commit %s would exceed model context window (%d tokens needed, %d available)", 
			commit.CommitID[:8], totalTokens + responseBuffer, contextSize)
		return models.NewCommitMessage{}, fmt.Errorf("commit would exceed model context window (%d tokens needed, %d available)", 
			totalTokens + responseBuffer, contextSize)
	}
	
	formatRaw := json.RawMessage(formatJSON)
	
	// Add commit as user message
	messages = append(messages, ollama.Message{Role: "user", Content: string(commitJSON)})

	ui.LogInfo("Sending commit %s to Ollama for processing (est. %d tokens)", commit.CommitID[:8], totalTokens)
	resp, err := SendOllamaMessage(model, messages, formatRaw, temperature)
	if err != nil {
		ui.LogError("Failed to send Ollama message: %v", err)
		return models.NewCommitMessage{}, fmt.Errorf("Failed to send Ollama message: %v", err)
	}

	var newCommit models.NewCommitMessage
	err = json.Unmarshal([]byte(resp), &newCommit)
	if err != nil {
		// Truncate the response if it's very large
		truncatedResp := resp
		if len(resp) > 1000 {
			truncatedResp = resp[:997] + "..."
		}
		
		// Log the raw response to provide more context for debugging
		ui.LogError("Failed to unmarshal Ollama response: %v", err)
		ui.LogError("Raw response (truncated):")
		for _, line := range strings.Split(truncatedResp, "\n") {
			ui.LogError("  %s", line)
		}
		return models.NewCommitMessage{}, fmt.Errorf("Failed to unmarshal Ollama response: %v. Check logs for details", err)
	}

	ui.UpdateStatus("Ready")
	return newCommit, nil
}