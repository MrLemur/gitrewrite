package commands

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/MrLemur/gitrewrite/internal/models"
	"github.com/MrLemur/gitrewrite/internal/services"
	"github.com/MrLemur/gitrewrite/internal/ui"
	"github.com/go-git/go-git/v5"
)

// Local reference to the model context size
var modelContextSize int

// Helper function to check if a file should be excluded
func shouldExcludeFile(path string, excludePattern *regexp.Regexp) bool {
	if excludePattern == nil {
		return false
	}
	return excludePattern.MatchString(path)
}

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

	// Verify the repository is on the main branch before proceeding
	ui.UpdateStatus("Checking repository branch...")
	ui.LogInfo("Verifying repository is on the main branch...")
	currentBranch, err := services.GetCurrentBranchName(RepoPath)
	if err != nil {
		ui.LogError("Failed to determine current branch: %v", err)
		ui.UpdateStatus("Error: Failed to determine current branch")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to determine current branch: %v", err)
	}
	
	// Get the default branch name from the repository
	defaultBranch, err := services.GetDefaultBranchName(RepoPath)
	if err != nil {
		ui.LogWarning("Failed to determine default branch, will use '%s' as reference: %v", currentBranch, err)
		defaultBranch = currentBranch // Fall back to current branch
	}
	
	if currentBranch != defaultBranch {
		ui.LogError("Repository must be on the default branch (%s) to proceed. Currently on: %s", defaultBranch, currentBranch)
		ui.UpdateStatus(fmt.Sprintf("Error: Repository must be on %s branch", defaultBranch))
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Repository must be on the default branch (%s) to proceed. Please checkout the default branch first.", defaultBranch)
	}
	ui.LogInfo("Verified repository is on the default branch: %s", defaultBranch)

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
	modelContextSize = contextSize // Use our local variable
	ui.LogInfo("Using context size of %d tokens for model %s", modelContextSize, Model)

	// Determine the output repository name
	var newRepoName string
	if OutputRepoName != "" {
		// Use the specified output repository name
		newRepoName = OutputRepoName
	} else {
		// Use the default name based on the original repository name
		repoName := services.GetRepoName(RepoPath)
		newRepoName = repoName + "-rewritten"
	}

	// We need the new repo path for later operations
	var newRepoPath string

	if DryRun {
		ui.LogInfo("Running in dry run mode - changes will not be applied")
	} else {
		ui.UpdateStatus("Creating new repository...")
		ui.LogInfo("Creating new repository with name %s", newRepoName)
		if err := services.CreateNewRepository(RepoPath, newRepoName, defaultBranch); err != nil {
			ui.LogError("Failed to create new repository: %v", err)
			ui.UpdateStatus("Error: Failed to create new repository")
			time.Sleep(2 * time.Second)
			ui.App.Stop()
			log.Fatalf("Failed to create new repository: %v", err)
		}
		
		// Get the full path to the new repository
		absSourcePath, err := filepath.Abs(RepoPath)
		if err != nil {
			ui.LogWarning("Failed to get absolute path for source repository: %v", err)
			absSourcePath = filepath.Clean(RepoPath)
		} else {
			absSourcePath = filepath.Clean(absSourcePath)
		}
		sourceParentDir := filepath.Dir(absSourcePath)
		newRepoPath = filepath.Join(sourceParentDir, newRepoName)
		ui.LogInfo("New repository located at %s", newRepoPath)
		
		// Configure the new repository with same branch name and remote as source
		ui.UpdateStatus("Configuring new repository...")
		ui.LogInfo("Configuring new repository to match source...")
		if err := services.ConfigureNewRepository(RepoPath, newRepoPath); err != nil {
			ui.LogError("Failed to configure new repository: %v", err)
			ui.UpdateStatus("Warning: Could not fully configure new repository")
			// We continue here as this is not a critical error
		}
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

	// Compile the exclude pattern if provided
	var excludePattern *regexp.Regexp
	if ExcludeFiles != "" {
		var err error
		excludePattern, err = regexp.Compile(ExcludeFiles)
		if err != nil {
			ui.LogError("Invalid exclude pattern: %v", err)
			ui.UpdateStatus("Error: Invalid exclude pattern")
			time.Sleep(2 * time.Second)
			ui.App.Stop()
			log.Fatalf("Invalid exclude pattern: %v", err)
		}
		ui.LogInfo("Using exclude pattern: %s", ExcludeFiles)
	}

	// Get commits to rewrite in chronological order (oldest to newest)
	ui.UpdateStatus("Getting commits in chronological order...")
	allCommits, commitsToRewrite, err := services.GetCommitsChronological(repo, MaxMsgLength, MaxDiffLength)
	if err != nil {
		ui.LogError("Failed to get commits in chronological order: %v", err)
		ui.UpdateStatus("Error: Failed to get commits")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to get commits from repository at %s: %v", RepoPath, err)
	}

	ui.TotalCommits = len(allCommits)
	ui.ProcessedCommits = 0
	ui.StartTime = time.Now()
	ui.TotalProcessingTime = 0
	ui.CommitTimings = make([]time.Duration, 0, ui.TotalCommits)
	ui.UpdateProgressBar()
	ui.LogInfo("Found %d total commits, %d need rewriting", ui.TotalCommits, len(commitsToRewrite))
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
			for _, commit := range commitsToRewrite {
				if !containsCommitID(processedCommitIDs, commit.CommitID) {
					remainingCommits = append(remainingCommits, commit)
				}
			}

			ui.LogInfo("Skipping %d already processed commits", len(commitsToRewrite)-len(remainingCommits))
			commitsToRewrite = remainingCommits
			ui.ProcessedCommits = len(existingOutputs)
			ui.UpdateProgressBar()
		}
	}

	// If not in dry run mode, calculate the new repo path for the confirmation message
	if !DryRun && newRepoPath == "" {
		absSourcePath, err := filepath.Abs(RepoPath)
		if err != nil {
			ui.LogWarning("Failed to get absolute path for source repository: %v", err)
			absSourcePath = filepath.Clean(RepoPath)
		} else {
			absSourcePath = filepath.Clean(absSourcePath)
		}
		sourceParentDir := filepath.Dir(absSourcePath)
		newRepoPath = filepath.Join(sourceParentDir, newRepoName)
	}

	// Add confirmation dialog if not in dry run mode
	if !DryRun {
		confirmMessage := fmt.Sprintf("%d total commits found, %d will be rewritten with improved messages. All commits will be applied to a new repository at %s.\n\nThis operation will create a new repository with the same files but improved commit messages.\n\n'No' is selected by default. Use Tab to select 'Yes' if you want to proceed.", ui.TotalCommits, len(commitsToRewrite), newRepoPath)
		confirmed := ui.ShowConfirmationDialog(confirmMessage)
		if !confirmed {
			ui.LogInfo("User cancelled the operation. Exiting.")
			ui.App.Stop()
			os.Exit(0)
		}
	}

	ui.LastCommitDetails.SetText("[yellow]No commits processed yet[white]")

	// Start a goroutine to process all commits
	go func() {
		for _, commit := range allCommits {
			shortID := commit.CommitID[:8]

			// For commits that don't need rewriting, just apply them with the original message
			if !commit.NeedsRewrite {
				if !DryRun {
					ui.LogInfo("Applying commit %s with original message (no rewrite needed)...", shortID)
					ui.UpdateStatus(fmt.Sprintf("Applying commit %s...", shortID))

					if err := services.ApplyCommitToNewRepo(repo, newRepoPath, commit.CommitID, commit.Message); err != nil {
						ui.LogError("Failed to apply commit %s to new repository: %v", shortID, err)
						continue
					}

					ui.LogSuccess("Successfully applied commit %s with original message", shortID)
				}
				ui.ProcessedCommits++
				ui.UpdateProgressBar()
				continue
			}

			// For commits that need rewriting, process them

			// Apply file exclusion pattern if needed
			if excludePattern != nil {
				var filteredFiles []models.File
				for _, file := range commit.Files {
					if !shouldExcludeFile(file.Path, excludePattern) {
						filteredFiles = append(filteredFiles, file)
					}
				}

				skipCount := len(commit.Files) - len(filteredFiles)
				if skipCount > 0 {
					ui.LogInfo("Excluded %d files matching pattern from commit %s", skipCount, shortID)
				}
				commit.Files = filteredFiles
			}

			if len(commit.Files) > MaxFilesPerCommit {
				if SummarizeOversizedCommits {
					ui.LogInfo("Commit %s has %d files (exceeding limit of %d). Generating simplified summary...", shortID, len(commit.Files), MaxFilesPerCommit)
					ui.UpdateStatus(fmt.Sprintf("Processing oversized commit %s...", shortID))

					ui.UpdateCommitDetails(commit.CommitID, len(commit.Files), -1, commit.Message, "Processing...")
					ui.LastCommitStartTime = time.Now()

					newMessage, err := services.GenerateSimplifiedCommitMessage(commit, Model, Temperature, modelContextSize)
					commitProcessingTime := time.Since(ui.LastCommitStartTime)

					if err != nil {
						ui.LogError("Failed to generate simplified commit message for %s: %v", shortID, err)
						continue
					}

					ui.UpdateCommitDetails(commit.CommitID, len(commit.Files), -1, strings.TrimSpace(commit.Message), newMessage)
					ui.LogInfo("Simplified commit message for %s generated successfully", shortID)

					if DryRun {
						rewriteOutput := models.RewriteOutput{
							CommitID:     commit.CommitID,
							OriginalMsg:  strings.TrimSpace(commit.Message),
							RewrittenMsg: newMessage,
							FilesChanged: len(commit.Files),
							IsApplied:    false,
						}
						rewriteOutputs = append(rewriteOutputs, rewriteOutput)
						ui.LogInfo("Added oversized commit %s to dry run output", shortID)
					} else {
						// Apply the commit to the new repository
						ui.UpdateStatus(fmt.Sprintf("Applying oversized commit %s to new repository...", shortID))
						if err := services.ApplyCommitToNewRepo(repo, newRepoPath, commit.CommitID, newMessage); err != nil {
							ui.LogError("Failed to apply oversized commit %s to new repository: %v", shortID, err)
							continue
						}

						ui.TotalProcessingTime += commitProcessingTime
						ui.CommitTimings = append(ui.CommitTimings, commitProcessingTime)
						ui.LogSuccess("Successfully applied oversized commit %s to new repository", shortID)
					}
					ui.ProcessedCommits++
					ui.UpdateProgressBar()
				} else {
					ui.LogError("Skipping commit with too many files (%d) for processing. Use -summarize-oversized to process it.", len(commit.Files))
					continue
				}
			} else {
				if ui.ProcessedCommits > 0 {
					ui.MoveToLastCommit()
				}

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

					// Save progress periodically (every 5 commits)
					if ui.ProcessedCommits%5 == 0 {
						savePartialDryRunResults(outputFilePath, rewriteOutputs)
					}
				} else {
					// Apply the commit to the new repository
					ui.UpdateStatus(fmt.Sprintf("Applying commit %s to new repository...", shortID))
					if err := services.ApplyCommitToNewRepo(repo, newRepoPath, commit.CommitID, newMessage); err != nil {
						ui.LogError("Failed to apply commit %s to new repository: %v", shortID, err)
						continue
					}

					// Update timing statistics
					ui.TotalProcessingTime += commitProcessingTime
					ui.CommitTimings = append(ui.CommitTimings, commitProcessingTime)
					ui.LogSuccess("Successfully applied commit %s to new repository", shortID)
				}
				ui.ProcessedCommits++
				ui.UpdateProgressBar()
			}
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
		} else if !DryRun {
			ui.UpdateStatus("All commits processed. New repository created at " + newRepoPath + ". Press Ctrl+C to exit")
			ui.LogInfo("Finished creating new repository with rewritten commits at %s", newRepoPath)
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
func ApplyChangesMode(repoPath, changesFile string) error {
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

	// Verify the repository is on the main branch before proceeding
	ui.UpdateStatus("Checking repository branch...")
	ui.LogInfo("Verifying repository is on the main branch...")
	currentBranch, err := services.GetCurrentBranchName(repoPath)
	if err != nil {
		ui.LogError("Failed to determine current branch: %v", err)
		ui.UpdateStatus("Error: Failed to determine current branch")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to determine current branch: %v", err)
	}
	
	// Get the default branch name from the repository
	defaultBranch, err := services.GetDefaultBranchName(repoPath)
	if err != nil {
		ui.LogWarning("Failed to determine default branch, will use '%s' as reference: %v", currentBranch, err)
		defaultBranch = currentBranch // Fall back to current branch
	}
	
	if currentBranch != defaultBranch {
		ui.LogError("Repository must be on the default branch (%s) to proceed. Currently on: %s", defaultBranch, currentBranch)
		ui.UpdateStatus(fmt.Sprintf("Error: Repository must be on %s branch", defaultBranch))
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Repository must be on the default branch (%s) to proceed. Please checkout the default branch first.", defaultBranch)
	}
	ui.LogInfo("Verified repository is on the default branch: %s", defaultBranch)

	// Read and parse the JSON file
	data, err := os.ReadFile(changesFile)
	if err != nil {
		ui.LogError("Failed to read changes file: %v", err)
		ui.UpdateStatus("Error: Failed to read changes file")
		return err
	}
	var changes []models.RewriteOutput
	if err := json.Unmarshal(data, &changes); err != nil {
		ui.LogError("Failed to parse changes file: %v", err)
		ui.UpdateStatus("Error: Failed to parse changes file")
		return err
	}
	ui.LogInfo("Loaded %d change entries from %s", len(changes), changesFile)

	// Determine the output repository name
	var newRepoName string
	if OutputRepoName != "" {
		// Use the specified output repository name
		newRepoName = OutputRepoName
	} else {
		// Use the default name based on the original repository name
		repoName := services.GetRepoName(repoPath)
		newRepoName = repoName + "-rewritten"
	}

	ui.UpdateStatus("Creating new repository...")
	ui.LogInfo("Creating new repository with name %s", newRepoName)
	if err := services.CreateNewRepository(repoPath, newRepoName, defaultBranch); err != nil {
		ui.LogError("Failed to create new repository: %v", err)
		ui.UpdateStatus("Error: Failed to create new repository")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to create new repository: %v", err)
	}
	
	// Get the full path to the new repository
	absSourcePath, err := filepath.Abs(repoPath)
	if err != nil {
		ui.LogWarning("Failed to get absolute path for source repository: %v", err)
		absSourcePath = filepath.Clean(repoPath)
	} else {
		absSourcePath = filepath.Clean(absSourcePath)
	}
	sourceParentDir := filepath.Dir(absSourcePath)
	newRepoPath := filepath.Join(sourceParentDir, newRepoName)
	ui.LogInfo("New repository located at %s", newRepoPath)
	
	// Configure the new repository with same branch name and remote as source
	ui.UpdateStatus("Configuring new repository...")
	ui.LogInfo("Configuring new repository to match source...")
	if err := services.ConfigureNewRepository(repoPath, newRepoPath); err != nil {
		ui.LogError("Failed to configure new repository: %v", err)
		ui.UpdateStatus("Warning: Could not fully configure new repository")
		// We continue here as this is not a critical error
	}

	// First get all commits to ensure we include those not being rewritten
	ui.UpdateStatus("Getting all commits...")
	allCommits, _, err := services.GetCommitsChronological(repo, MaxMsgLength, MaxDiffLength)
	if err != nil {
		ui.LogError("Failed to get all commits: %v", err)
		ui.UpdateStatus("Error: Failed to get all commits")
		time.Sleep(2 * time.Second)
		ui.App.Stop()
		log.Fatalf("Failed to get all commits: %v", err)
	}

	// Build a map of commit IDs to their new messages
	rewriteMap := make(map[string]string)
	for _, change := range changes {
		rewriteMap[change.CommitID] = change.RewrittenMsg
	}

	ui.TotalCommits = len(allCommits)
	ui.ProcessedCommits = 0
	ui.StartTime = time.Now()
	ui.TotalProcessingTime = 0
	ui.CommitTimings = make([]time.Duration, 0, ui.TotalCommits)
	ui.UpdateProgressBar()

	if ui.TotalCommits > 0 {
		confirmMessage := fmt.Sprintf("%d total commits will be processed, %d with improved messages from file. All will be applied to a new repository at %s.\n\nThis operation will create a new repository with the same files but improved commit messages.\n\n'No' is selected by default. Use Tab to select 'Yes' if you want to proceed.", ui.TotalCommits, len(changes), newRepoPath)
		confirmed := ui.ShowConfirmationDialog(confirmMessage)
		if !confirmed {
			ui.LogInfo("User cancelled the operation. Exiting.")
			ui.App.Stop()
			os.Exit(0)
		}
	}

	// Process all commits in chronological order
	for _, commit := range allCommits {
		commitID := commit.CommitID
		shortID := commitID[:8]

		// Check if this commit has a rewritten message in the changes file
		newMessage, hasRewrite := rewriteMap[commitID]

		if hasRewrite {
			ui.LogInfo("Applying commit %s with rewritten message...", shortID)
			ui.UpdateStatus(fmt.Sprintf("Applying commit %s with rewritten message...", shortID))
			ui.UpdateCommitDetails(commitID, 0, -1, commit.Message, newMessage)
		} else {
			ui.LogInfo("Applying commit %s with original message...", shortID)
			ui.UpdateStatus(fmt.Sprintf("Applying commit %s with original message...", shortID))
			newMessage = commit.Message
		}

		ui.LastCommitStartTime = time.Now()
		// Apply the commit to the new repository
		if err := services.ApplyCommitToNewRepo(repo, newRepoPath, commitID, newMessage); err != nil {
			ui.LogError("Failed to apply commit %s to new repository: %v", shortID, err)
			continue
		}

		if hasRewrite {
			ui.LogSuccess("Successfully applied commit %s with rewritten message", shortID)
		} else {
			ui.LogSuccess("Successfully applied commit %s with original message", shortID)
		}

		commitProcessingTime := time.Since(ui.LastCommitStartTime)
		ui.TotalProcessingTime += commitProcessingTime
		ui.CommitTimings = append(ui.CommitTimings, commitProcessingTime)

		ui.ProcessedCommits++
		ui.UpdateProgressBar()
	}

	ui.UpdateStatus("All changes applied. New repository created at " + newRepoPath + ". Press Ctrl+C to exit")
	ui.LogInfo("Finished creating new repository with rewritten commits at %s", newRepoPath)
	return nil
}