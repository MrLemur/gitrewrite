package commands

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/MrLemur/gitrewrite/internal/models"
	"github.com/MrLemur/gitrewrite/internal/services"
	"github.com/MrLemur/gitrewrite/internal/ui"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Local reference to the model context size
var modelContextSize int 

// RunApplication runs the main application logic
func RunApplication() {
	if RepoPath == "" {
		fmt.Println("Please provide a path to a git repository using -repo=/path/to/repo")
		os.Exit(1)
	}

	// If apply-changes mode is specified, run that mode and exit afterward.
	if ApplyChangesFile != "" {
		ui.LogInfo("Running in apply-changes mode using file: %s", ApplyChangesFile)
		ApplyChangesMode(RepoPath, ApplyChangesFile)
		ui.UpdateStatus("Press Ctrl+C to exit")
		select {}
	}

	// Check Ollama availability and get model context size
	ui.UpdateStatus("Checking Ollama availability...")
	ui.LogInfo("Checking if Ollama is available...")
	if err := services.CheckOllamaAvailability(); err != nil {
		ui.LogError("Failed to connect to Ollama: %v", err)
		ui.UpdateStatus("Error: Failed to connect to Ollama")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to connect to Ollama: %v", err)
	}
	
	ui.UpdateStatus("Getting model information...")
	ui.LogInfo("Getting context size for model: %s", Model)
	contextSize, err := services.GetModelContextSize(Model)
	if err != nil {
		ui.LogError("Failed to get context size for model %s: %v", Model, err)
		ui.UpdateStatus("Error: Failed to determine model context size")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to determine context size for model %s: %v", Model, err)
	}
	modelContextSize = contextSize  // Use our local variable
	ui.LogInfo("Using context size of %d tokens for model %s", modelContextSize, Model)

	if DryRun {
		ui.LogInfo("Running in dry run mode - changes will not be applied")
	}

	var outputFilePath string
	if DryRun {
		if OutputFile != "" {
			outputFilePath = OutputFile
		} else {
			repoName := services.GetRepoName(RepoPath)
			outputFilePath = fmt.Sprintf("%s-rewrite-changes.json", repoName)
		}
		ui.LogInfo("Dry run results will be saved to %s", outputFilePath)
	}

	ui.UpdateStatus("Opening repository...")
	ui.LogInfo("Opening git repository at %s", RepoPath)
	repo, err := git.PlainOpen(RepoPath)
	if err != nil {
		ui.LogError("Failed to open repository: %v", err)
		ui.UpdateStatus("Error: Failed to open repository")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to open repository at %s: %v", RepoPath, err)
	}


	// Get commits to rewrite
	oldCommits, err := services.GetCommitsToRewrite(repo, MaxMsgLength, MaxDiffLength)
	if err != nil {
		ui.LogError("Failed to iterate over commits: %v", err)
		ui.UpdateStatus("Error: Failed to iterate over commits")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to iterate over commits in repository at %s: %v", RepoPath, err)
	}

	ui.TotalCommits = len(oldCommits)
	ui.ProcessedCommits = 0
	ui.StartTime = time.Now()
	ui.TotalProcessingTime = 0
	ui.CommitTimings = make([]time.Duration, 0, ui.TotalCommits)
	ui.UpdateProgressBar()
	ui.LogInfo("Found %d commits that need rewriting", ui.TotalCommits)
	if ui.TotalCommits == 0 {
		ui.LogInfo("No commits to process. Exiting.")
		ui.UpdateStatus("No commits to process. Press Ctrl+C to exit")
		select {}
	}

	// Set up a channel to catch interrupt signals for clean exit
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    
	// Set up a tracker for completion
	done := make(chan bool, 1)
    
	var rewriteOutputs []models.RewriteOutput
    
	// Check if we have an existing dry run file to resume from
	if DryRun {
		// Try to load existing dry run results
		existingOutputs, processedCommitIDs := loadExistingDryRunResults(outputFilePath)
		if len(existingOutputs) > 0 {
			ui.LogInfo("Found existing dry run results with %d processed commits. Resuming...", len(existingOutputs))
			rewriteOutputs = existingOutputs
            
			// Filter out already processed commits
			var remainingCommits []models.CommitOutput
			for _, commit := range oldCommits {
				if !containsCommitID(processedCommitIDs, commit.CommitID) {
					remainingCommits = append(remainingCommits, commit)
				}
			}
            
			ui.LogInfo("Skipping %d already processed commits", len(oldCommits)-len(remainingCommits))
			oldCommits = remainingCommits
			ui.ProcessedCommits = len(existingOutputs)
			ui.UpdateProgressBar()
		}
	}

	// Add confirmation dialog if we're not in dry run mode
	if !DryRun {
		confirmMessage := fmt.Sprintf("%d commits have been found and will be processed.\n\nWARNING: This process is irreversible and will modify your git history.\n\n'No' is selected by default. Use Tab to select 'Yes' if you want to proceed.", ui.TotalCommits)
		confirmed := ui.ShowConfirmationDialog(confirmMessage)
		if !confirmed {
			ui.LogInfo("User cancelled the operation. Exiting.")
			ui.App.Stop()
			os.Exit(0)
		}
	}

	ui.LastCommitDetails.SetText("[yellow]No commits processed yet[white]")
	var rewriteOutputs []models.RewriteOutput

	for _, commit := range oldCommits {
		if len(commit.Files) > 200 {
			ui.LogError("Skipping commit with too many files (%d) for processing.", len(commit.Files))
			continue
		}
		if ui.ProcessedCommits > 0 {
			ui.MoveToLastCommit()
		}
		shortID := commit.CommitID[:8]
		ui.LogInfo("Processing commit %s...", shortID)
		ui.UpdateStatus(fmt.Sprintf("Processing commit %s...", shortID))
		
		// Calculate total diff size for this commit
		totalDiffSize := 0
		for _, file := range commit.Files {
			totalDiffSize += len(file.Diff)
		}
		
		ui.UpdateCommitDetails(commit.CommitID, len(commit.Files), totalDiffSize, commit.Message, "Processing...")
		ui.LastCommitStartTime = time.Now()
		newCommit, err := services.GenerateNewCommitMessage(commit, Model, Temperature, modelContextSize)
		commitProcessingTime := time.Since(ui.LastCommitStartTime)
		if err != nil {
			ui.LogError("Failed to generate new commit message for %s: %v", shortID, err)
			continue
		}
		var newMessageLines []string
		for _, msg := range newCommit.Messages {
			if !(msg["type"] == "feat" || msg["type"] == "fix" || msg["type"] == "chore" || msg["type"] == "docs" || msg["type"] == "refactor" || msg["type"] == "perf") {
				continue
			}
			line := fmt.Sprintf("%s: %s (%s)", msg["type"], msg["description"], msg["affected_app"])
			newMessageLines = append(newMessageLines, line)
		}
		newMessage := strings.Join(newMessageLines, "\n\r")
		ui.UpdateCommitDetails(commit.CommitID, len(commit.Files), totalDiffSize, strings.TrimSpace(commit.Message), newMessage)
		ui.LogInfo("New commit message for %s generated successfully", shortID)
		if DryRun {
			rewriteOutput := models.RewriteOutput{
				CommitID:     commit.CommitID,
				OriginalMsg:  strings.TrimSpace(commit.Message),
				RewrittenMsg: newMessage,
				FilesChanged: len(commit.Files),
				IsApplied:    false,
			}
			rewriteOutputs = append(rewriteOutputs, rewriteOutput)
			ui.LogInfo("Added commit %s to dry run output", shortID)
		} else {
			if err := services.RewordCommit(RepoPath, newCommit.CommitID, newMessage); err != nil {
				ui.LogError("Failed to reword commit %s: %v", shortID, err)
				continue
			}
			
			// Update timing statistics
			ui.TotalProcessingTime += commitProcessingTime
			ui.CommitTimings = append(ui.CommitTimings, commitProcessingTime)
			ui.LogSuccess("Successfully rewrote commit %s", shortID)
		}
		ui.ProcessedCommits++
		ui.UpdateProgressBar()
	}
            
			// Calculate total diff size for this commit
			totalDiffSize := 0
			for _, file := range commit.Files {
				totalDiffSize += len(file.Diff)
			}
            
			ui.UpdateCommitDetails(commit.CommitID, len(commit.Files), totalDiffSize, commit.Message, "Processing...")
			ui.LastCommitStartTime = time.Now()
			newCommit, err := services.GenerateNewCommitMessage(commit, Model, Temperature, modelContextSize)
			commitProcessingTime := time.Since(ui.LastCommitStartTime)
			if err != nil {
				ui.LogError("Failed to generate new commit message for %s: %v", shortID, err)
				continue
			}
			var newMessageLines []string
			for _, msg := range newCommit.Messages {
				if !(msg["type"] == "feat" || msg["type"] == "fix" || msg["type"] == "chore" || msg["type"] == "docs" || msg["type"] == "refactor" || msg["type"] == "perf") {
					continue
				}
				line := fmt.Sprintf("%s: %s (%s)", msg["type"], msg["description"], msg["affected_app"])
				newMessageLines = append(newMessageLines, line)
			}
			newMessage := strings.Join(newMessageLines, "\n\r")
			ui.UpdateCommitDetails(commit.CommitID, len(commit.Files), totalDiffSize, strings.TrimSpace(commit.Message), newMessage)
			ui.LogInfo("New commit message for %s generated successfully", shortID)
			if DryRun {
				rewriteOutput := models.RewriteOutput{
					CommitID:     commit.CommitID,
					OriginalMsg:  strings.TrimSpace(commit.Message),
					RewrittenMsg: newMessage,
					FilesChanged: len(commit.Files),
					IsApplied:    false,
				}
				rewriteOutputs = append(rewriteOutputs, rewriteOutput)
				ui.LogInfo("Added commit %s to dry run output", shortID)
                
				// Save progress periodically (every 5 commits)
				if ui.ProcessedCommits % 5 == 0 {
					savePartialDryRunResults(outputFilePath, rewriteOutputs)
				}
			} else {
				if err := services.RewordCommit(RepoPath, newCommit.CommitID, newMessage); err != nil {
					ui.LogError("Failed to reword commit %s: %v", shortID, err)
					continue
				}
                
				// Update timing statistics
				ui.TotalProcessingTime += commitProcessingTime
				ui.CommitTimings = append(ui.CommitTimings, commitProcessingTime)
				ui.LogSuccess("Successfully rewrote commit %s", shortID)
			}
			ui.ProcessedCommits++
			ui.UpdateProgressBar()
		}

		if DryRun && len(rewriteOutputs) > 0 {
			ui.UpdateStatus("Saving dry run results...")
			ui.LogInfo("Saving dry run results to %s", outputFilePath)
			outputData, err := json.MarshalIndent(rewriteOutputs, "", "  ")
			if err != nil {
				ui.LogError("Failed to marshal dry run results: %v", err)
				ui.UpdateStatus("Error: Failed to save dry run results")
			} else {
				err = os.WriteFile(outputFilePath, outputData, 0644)
				if err != nil {
					ui.LogError("Failed to write dry run results to file: %v", err)
					ui.UpdateStatus("Error: Failed to save dry run results")
				} else {
					ui.LogSuccess("Dry run results saved successfully to %s", outputFilePath)
					ui.UpdateStatus("Dry run completed. Press Ctrl+C to exit")
				}
			}
		} else {
			ui.UpdateStatus("All commits processed. Press Ctrl+C to exit")
			ui.LogInfo("Finished rewriting all commits")
		}
        
		// Signal that we're done processing
		done <- true
	}()

	// Wait for either completion or interrupt
	select {
	case <-sigs:
		// Handle clean shutdown on interrupt
		ui.LogInfo("Received interrupt signal, shutting down...")
		if DryRun && len(rewriteOutputs) > 0 {
			ui.UpdateStatus("Saving partial dry run results...")
			ui.LogInfo("Saving partial dry run results to %s", outputFilePath)
			savePartialDryRunResults(outputFilePath, rewriteOutputs)
		}
		ui.App.Stop()
		os.Exit(0)
	case <-done:
		// Wait for user to exit
		select {}
	}
}

// Helper function to check if a commit ID is in a slice
func containsCommitID(ids []string, id string) bool {
	for _, existingID := range ids {
		if existingID == id {
			return true
		}
	}
	return false
}

// Helper function to load existing dry run results
func loadExistingDryRunResults(filePath string) ([]models.RewriteOutput, []string) {
	var outputs []models.RewriteOutput
	var commitIDs []string
    
	data, err := os.ReadFile(filePath)
	if err != nil {
		// File doesn't exist or can't be read, return empty results
		return outputs, commitIDs
	}
    
	if err := json.Unmarshal(data, &outputs); err != nil {
		ui.LogError("Failed to parse existing dry run file: %v", err)
		return outputs, commitIDs
	}
    
	// Extract commit IDs for quick lookup
	for _, output := range outputs {
		commitIDs = append(commitIDs, output.CommitID)
	}
    
	return outputs, commitIDs
}

// Helper function to save partial dry run results
func savePartialDryRunResults(filePath string, outputs []models.RewriteOutput) {
	if len(outputs) == 0 {
		return
	}
    
	outputData, err := json.MarshalIndent(outputs, "", "  ")
	if err != nil {
		ui.LogError("Failed to marshal partial dry run results: %v", err)
		return
	}
    
	err = os.WriteFile(filePath, outputData, 0644)
	if err != nil {
		ui.LogError("Failed to write partial dry run results to file: %v", err)
		return
	}
    
	ui.LogInfo("Saved partial dry run results with %d commits to %s", len(outputs), filePath)
}

// ApplyChangesMode reads a JSON file with rewrite outputs and applies each change
func ApplyChangesMode(repoPath, changesFile string) {
	ui.UpdateStatus("Applying changes from file...")
	ui.LogInfo("Opening repository at %s", repoPath)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		ui.LogError("Failed to open repository: %v", err)
		ui.UpdateStatus("Error: Failed to open repository")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to open repository at %s: %v", repoPath, err)
	}

	// Read and parse the JSON file
	data, err := os.ReadFile(changesFile)
	if err != nil {
		ui.LogError("Failed to read changes file: %v", err)
		ui.UpdateStatus("Error: Failed to read changes file")
		return
	}
	var changes []models.RewriteOutput
	if err := json.Unmarshal(data, &changes); err != nil {
		ui.LogError("Failed to parse changes file: %v", err)
		ui.UpdateStatus("Error: Failed to parse changes file")
		return
	}
	ui.LogInfo("Loaded %d change entries from %s", len(changes), changesFile)

	// For safer rebase operations, process changes from oldest to newest.
	// We sort by commit date by retrieving each commit object.
	type commitWithTime struct {
		output models.RewriteOutput
		time   time.Time
	}
	var commits []commitWithTime
	for _, change := range changes {
		commitObj, err := repo.CommitObject(plumbing.NewHash(change.CommitID))
		if err != nil {
			// If not found by hash, try to find by original message.
			foundID, err := services.FindCommitByMessage(repo, change.OriginalMsg)
			if err != nil {
				ui.LogError("Commit %s not found: %v", change.CommitID, err)
				continue
			}
			commitObj, err = repo.CommitObject(plumbing.NewHash(foundID))
			if err != nil {
				ui.LogError("Error retrieving commit %s: %v", foundID, err)
				continue
			}
			change.CommitID = foundID
		}
		commits = append(commits, commitWithTime{output: change, time: commitObj.Committer.When})
	}
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].time.Before(commits[j].time)
	})
	ui.TotalCommits = len(commits)
	ui.ProcessedCommits = 0
	ui.StartTime = time.Now()
	ui.TotalProcessingTime = 0
	ui.CommitTimings = make([]time.Duration, 0, ui.TotalCommits)
	ui.UpdateProgressBar()

	if ui.TotalCommits > 0 {
		confirmMessage := fmt.Sprintf("%d commits from file will be processed.\n\nWARNING: This process is irreversible and will modify your git history.\n\n'No' is selected by default. Use Tab to select 'Yes' if you want to proceed.", ui.TotalCommits)
		confirmed := ui.ShowConfirmationDialog(confirmMessage)
		if !confirmed {
			ui.LogInfo("User cancelled the operation. Exiting.")
			ui.App.Stop()
			os.Exit(0)
		}
	}

	// Process each change entry
	for _, entry := range commits {
		change := entry.output
		var targetID string
		// First, try to look up the commit by the stored hash.
		_, err := repo.CommitObject(plumbing.NewHash(change.CommitID))
		if err != nil {
			// If not found, search by original message.
			foundID, err := services.FindCommitByMessage(repo, change.OriginalMsg)
			if err != nil {
				ui.LogError("Skipping change for commit with original message '%s': %v", change.OriginalMsg, err)
				continue
			}
			targetID = foundID
			ui.LogInfo("Found commit by message for original commit id %s: using %s", change.CommitID[:8], targetID[:8])
		} else {
			targetID = change.CommitID
		}

		// Sanity check: verify that the commit's current message matches the expected original message.
		commitObj, err := repo.CommitObject(plumbing.NewHash(targetID))
		if err != nil {
			ui.LogError("Failed to retrieve commit %s: %v", targetID, err)
			continue
		}
		if strings.TrimSpace(commitObj.Message) != strings.TrimSpace(change.OriginalMsg) {
			ui.LogError("Commit %s original message does not match expected value; skipping", targetID[:8])
			continue
		}

		ui.LogInfo("Rewriting commit %s...", targetID[:8])
		ui.UpdateStatus(fmt.Sprintf("Rewriting commit %s...", targetID[:8]))
		
		// For apply-changes mode, we don't have the full diff information
		// so we'll show N/A for the diff size
		if change.FilesChanged > 0 {
			ui.UpdateCommitDetails(targetID, change.FilesChanged, -1, strings.TrimSpace(change.OriginalMsg), change.RewrittenMsg)
		} else {
			ui.UpdateCommitDetails(targetID, 0, -1, strings.TrimSpace(change.OriginalMsg), change.RewrittenMsg)
		}
		
		ui.LastCommitStartTime = time.Now()
		if err := services.RewordCommit(repoPath, targetID, change.RewrittenMsg); err != nil {
			ui.LogError("Failed to reword commit %s: %v", targetID[:8], err)
		} else {
			ui.LogSuccess("Successfully rewrote commit %s", targetID[:8])
			// Record processing time for this commit
			commitProcessingTime := time.Since(ui.LastCommitStartTime)
			ui.TotalProcessingTime += commitProcessingTime
			ui.CommitTimings = append(ui.CommitTimings, commitProcessingTime)
		}
		ui.ProcessedCommits++
		ui.UpdateProgressBar()
	}
	ui.UpdateStatus("All changes applied. Press Ctrl+C to exit")
	ui.LogInfo("Finished applying all changes")
	select {} // Keep the app running until the user exits
}