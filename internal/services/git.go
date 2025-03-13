package services

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MrLemur/gitrewrite/internal/models"
	"github.com/MrLemur/gitrewrite/internal/ui"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// MockUpdateStatusForTests is a flag that can be set to disable UI updates during testing
var MockUpdateStatusForTests bool

// safeUpdateStatus updates the UI status only if we're not running in test mode
func safeUpdateStatus(text string) {
	if !MockUpdateStatusForTests {
		ui.UpdateStatus(text)
	}
}

// RewordCommit changes the message of a specific git commit
func RewordCommit(repoPath, targetCommit, newMessage string) error {
	safeUpdateStatus("Rewriting commit message...")
	// Ensure we're in a git repository
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not a git repository")
	}

	// Determine the rebase base
	parentCmd := exec.Command("git", "rev-parse", targetCommit+"^")
	parentCmd.Dir = repoPath
	parentOutput, err := parentCmd.Output()

	var base string
	if err != nil {
		base = "--root"
	} else {
		base = strings.TrimSpace(string(parentOutput))
	}

	// Get abbreviated hash for target commit
	abbrCmd := exec.Command("git", "rev-parse", "--short", targetCommit)
	abbrCmd.Dir = repoPath
	abbrOutput, err := abbrCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get abbreviated hash for commit: %v", err)
	}

	var shortHash string
	if err != nil {
		shortHash = targetCommit
	} else {
		shortHash = strings.TrimSpace(string(abbrOutput))
	}

	// Set up GIT_SEQUENCE_EDITOR to change "pick" to "reword" for the target commit
	escapedTarget := regexp.QuoteMeta(targetCommit)
	sedExpr := fmt.Sprintf("s/^pick \\(%s\\|%s\\)/reword \\1/", escapedTarget, shortHash)
	gitSeqEditor := fmt.Sprintf("sed -i -e \"%s\"", sedExpr)

	// Create temporary editor script to provide the new commit message
	tempEditor, err := os.CreateTemp("", "git-editor-")
	if err != nil {
		return fmt.Errorf("failed to create temp editor script: %v", err)
	}
	defer os.Remove(tempEditor.Name())

	// Create a temporary file to store the new commit message
	tempFile, err := os.CreateTemp("", "new-commit-message-")
	if err != nil {
		return fmt.Errorf("failed to create temp file for new commit message: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write the new commit message to a temporary file
	if _, err := tempFile.WriteString(newMessage); err != nil {
		return fmt.Errorf("failed to write new commit message: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	editorContent := fmt.Sprintf("#!/bin/sh\ncat %s > \"$1\"\n", tempFile.Name())
	if _, err := tempEditor.WriteString(editorContent); err != nil {
		return fmt.Errorf("failed to write temp editor: %v", err)
	}
	if err := tempEditor.Close(); err != nil {
		return fmt.Errorf("failed to close temp editor: %v", err)
	}
	if err := os.Chmod(tempEditor.Name(), 0755); err != nil {
		return fmt.Errorf("failed to make editor executable: %v", err)
	}

	// Prepare environment with our custom editors
	env := append(os.Environ(),
		"GIT_SEQUENCE_EDITOR="+gitSeqEditor,
		"GIT_EDITOR="+tempEditor.Name(),
	)

	// Remove any existing rebase-merge directory
	mergeDir := filepath.Join(repoPath, ".git", "rebase-merge")
	if _, err := os.Stat(mergeDir); err == nil {
		if err := os.RemoveAll(mergeDir); err != nil {
			return fmt.Errorf("failed to remove rebase-merge directory: %v", err)
		}
	}

	// Clear any existing rebase state
	clearCmd := exec.Command("git", "rebase", "--abort")
	clearCmd.Dir = repoPath
	clearCmd.Env = env
	output, err := clearCmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "No rebase in progress?") {
			// No rebase in progress, ignore
		} else {
			return fmt.Errorf("failed to clear rebase state: %v\nOutput: %s", err, output)
		}
	}

	// Execute rebase to rewrite the commit message
	args := []string{"rebase", "-i"}
	if base == "--root" {
		args = append(args, "--root")
	} else {
		args = append(args, base)
	}
	rebaseCmd := exec.Command("git", args...)
	rebaseCmd.Dir = repoPath
	rebaseCmd.Env = env

	output, err = rebaseCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rebase failed: %v\nOutput: %s", err, output)
	}

	safeUpdateStatus("Ready")
	return nil
}

// GetCommitsToRewrite gets a list of commits that need to be rewritten
func GetCommitsToRewrite(repo *git.Repository, maxMsgLength, maxDiffLength int) ([]models.CommitOutput, error) {
	safeUpdateStatus("Analyzing git history...")

	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get repository log: %v", err)
	}

	var commits []models.CommitOutput
	err = iter.ForEach(func(c *object.Commit) error {
		if len(c.Message) <= maxMsgLength {
			output := models.CommitOutput{
				CommitID: c.Hash.String(),
				Message:  c.Message,
			}
			parentCommits := c.Parents()
			var changes object.Changes
			firstParent, err := parentCommits.Next()
			if err == nil {
				parentTree, err := firstParent.Tree()
				if err != nil {
					return fmt.Errorf("failed to get parent tree for commit %s: %v", c.Hash.String(), err)
				}
				currentTree, err := c.Tree()
				if err != nil {
					return fmt.Errorf("failed to get current tree for commit %s: %v", c.Hash.String(), err)
				}
				changes, err = parentTree.Diff(currentTree)
				if err != nil {
					return fmt.Errorf("failed to compute diff for commit %s: %v", c.Hash.String(), err)
				}
			} else if err == io.EOF {
				currentTree, err := c.Tree()
				if err != nil {
					return fmt.Errorf("failed to get current tree for initial commit %s: %v", c.Hash.String(), err)
				}
				changes, err = object.DiffTree(nil, currentTree)
				if err != nil {
					return fmt.Errorf("failed to compute diff for initial commit %s: %v", c.Hash.String(), err)
				}
			} else {
				return fmt.Errorf("error getting parent commits for %s: %v", c.Hash.String(), err)
			}
			for _, change := range changes {
				_, _, err := change.Files()
				if err != nil {
					return fmt.Errorf("failed to get files for change: %v", err)
				}
				var path string
				if change.From.Name != "" {
					path = change.From.Name
				} else if change.To.Name != "" {
					path = change.To.Name
				} else {
					continue
				}
				patch, err := change.Patch()
				if err != nil {
					return fmt.Errorf("failed to generate patch for %s: %v", path, err)
				}
				diffContent := patch.String()
				if len(diffContent) > maxDiffLength {
					diffContent = diffContent[:maxDiffLength]
				}
				output.Files = append(output.Files, models.File{
					Path: path,
					Diff: diffContent,
				})
			}
			commits = append(commits, output)
		}
		return nil
	})

	return commits, err
}

// GetRepoName extracts the name of a repository from its path
func GetRepoName(repoPath string) string {
	if repoPath == "" {
		return "git-repo"
	}

	// If it's the current directory, get the actual directory name
	if repoPath == "." {
		absPath, err := filepath.Abs(repoPath)
		if err == nil {
			repoPath = absPath
		}
	}

	repoName := filepath.Base(repoPath)
	repoName = strings.TrimRight(repoName, "/\\")
	if repoName == "" || repoName == ".." {
		return "git-repo"
	}
	return repoName
}

// FindCommitByMessage searches for a commit with a specific message
func FindCommitByMessage(repo *git.Repository, origMsg string) (string, error) {
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return "", err
	}
	defer iter.Close()
	var found string
	err = iter.ForEach(func(c *object.Commit) error {
		if strings.TrimSpace(c.Message) == strings.TrimSpace(origMsg) {
			found = c.Hash.String()
			return io.EOF // break out of iteration
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("no commit found with message: %s", origMsg)
	}
	return found, nil
}

// ExecuteCommand runs a command and returns an error if it fails
func ExecuteCommand(command string, args []string, dir string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	return cmd.Run()
}

// GetCommandOutput runs a command and returns its output
func GetCommandOutput(command string, args []string, dir string) (string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}
