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
	"github.com/go-git/go-git/v5/plumbing"
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
	ui.LogShellCommand("git", []string{"rev-parse", "--git-dir"}, repoPath)
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not a git repository")
	}

	// Determine the rebase base
	ui.LogShellCommand("git", []string{"rev-parse", targetCommit + "^"}, repoPath)
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
	ui.LogShellCommand("git", []string{"rev-parse", "--short", targetCommit}, repoPath)
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
	ui.LogShellCommand("git", []string{"rebase", "--abort"}, repoPath)
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

	ui.LogShellCommand("git", args, repoPath)
	rebaseCmd := exec.Command("git", args...)
	rebaseCmd.Dir = repoPath
	rebaseCmd.Env = env

	ui.LogInfo("Command dir: %s", rebaseCmd.Dir)
	ui.LogInfo("Command env: %v", rebaseCmd.Env)
	// show temp editor content
	ui.LogInfo("Temp editor content: %s", editorContent)
	ui.LogInfo("Temp file content: %s", newMessage)

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

// ExecuteCommand runs a command and returns an error if it fails
func ExecuteCommand(command string, args []string, dir string) error {
	ui.LogShellCommand(command, args, dir)
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	return cmd.Run()
}

// GetCommandOutput runs a command and returns its output
func GetCommandOutput(command string, args []string, dir string) (string, error) {
	ui.LogShellCommand(command, args, dir)
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// CreateNewRepository creates a new empty git repository at the specified path
func CreateNewRepository(path string) error {
	// Check if the directory already exists
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("directory %s already exists", path)
	}

	// Create the directory
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", path, err)
	}

	// Initialize the repository
	ui.LogShellCommand("git", []string{"init"}, path)
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to initialize git repository: %v, output: %s", err, output)
	}

	return nil
}

// GetCommitsChronological returns ALL commits from oldest to newest
func GetCommitsChronological(repo *git.Repository, maxMsgLength, maxDiffLength int) ([]models.CommitOutput, []models.CommitOutput, error) {
	safeUpdateStatus("Getting commits in chronological order...")

	// Get all commits
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get repository log: %v", err)
	}

	var allCommits []models.CommitOutput
	var commitsToRewrite []models.CommitOutput

	err = iter.ForEach(func(c *object.Commit) error {
		output := models.CommitOutput{
			CommitID:     c.Hash.String(),
			Message:      c.Message,
			NeedsRewrite: len(c.Message) <= maxMsgLength,
		}

		// If commit needs rewriting, get the diff information
		if output.NeedsRewrite {
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

			commitsToRewrite = append(commitsToRewrite, output)
		}

		allCommits = append(allCommits, output)
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	// Reverse the order of commits to get chronological order (oldest first)
	reverseCommits := func(commits []models.CommitOutput) {
		for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
			commits[i], commits[j] = commits[j], commits[i]
		}
	}

	reverseCommits(allCommits)
	reverseCommits(commitsToRewrite)

	return allCommits, commitsToRewrite, nil
}

// ApplyCommitToNewRepo applies a commit from the original repo to the new repo
func ApplyCommitToNewRepo(originalRepo *git.Repository, newRepoPath, commitID, newMessage string) error {
	// Get the commit
	hash := plumbing.NewHash(commitID)
	commit, err := originalRepo.CommitObject(hash)
	if err != nil {
		return fmt.Errorf("failed to get commit object: %v", err)
	}

	// Get author info and timestamps
	authorName := commit.Author.Name
	authorEmail := commit.Author.Email
	authorWhen := commit.Author.When.Unix()
	committerWhen := commit.Committer.When.Unix()

	// Get the tree for this commit
	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("failed to get tree for commit: %v", err)
	}

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "gitrewrite-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract all files from the tree to the temp directory
	err = tree.Files().ForEach(func(f *object.File) error {
		// Get file contents
		content, err := f.Contents()
		if err != nil {
			return fmt.Errorf("failed to get contents of file %s: %v", f.Name, err)
		}

		// Create the target path
		targetPath := filepath.Join(tmpDir, f.Name)

		// Create the directory for the file
		err = os.MkdirAll(filepath.Dir(targetPath), 0755)
		if err != nil {
			return fmt.Errorf("failed to create directory for file %s: %v", f.Name, err)
		}

		// Write the file
		err = os.WriteFile(targetPath, []byte(content), 0644)
		if err != nil {
			return fmt.Errorf("failed to write file %s: %v", f.Name, err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to extract files: %v", err)
	}

	// Remove all files in the new repo (except .git)
	newRepoFiles, err := os.ReadDir(newRepoPath)
	if err != nil {
		return fmt.Errorf("failed to read new repo directory: %v", err)
	}

	for _, file := range newRepoFiles {
		if file.Name() != ".git" {
			pathToRemove := filepath.Join(newRepoPath, file.Name())
			err := os.RemoveAll(pathToRemove)
			if err != nil {
				return fmt.Errorf("failed to remove file %s: %v", pathToRemove, err)
			}
		}
	}

	// Copy all files from the temp directory to the new repo
	err = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory
		if path == tmpDir {
			return nil
		}

		// Get the relative path
		relPath, err := filepath.Rel(tmpDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %v", err)
		}

		// Create the target path
		targetPath := filepath.Join(newRepoPath, relPath)

		if info.IsDir() {
			// Create directory
			return os.MkdirAll(targetPath, info.Mode())
		} else {
			// Copy file
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %v", path, err)
			}

			return os.WriteFile(targetPath, data, info.Mode())
		}
	})

	if err != nil {
		return fmt.Errorf("failed to copy files: %v", err)
	}

	// Add all files to the new repo
	ui.LogShellCommand("git", []string{"add", "-A"}, newRepoPath)
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = newRepoPath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add files to new repo: %v, output: %s", err, output)
	}

	// Format the commit command with author info and timestamps
	authorArg := fmt.Sprintf("--author=%s <%s>", authorName, authorEmail)
	dateArg := fmt.Sprintf("--date=%d", authorWhen)

	// Commit with the new message and preserve author info and date
	commitCmd := exec.Command("git", "commit", "--allow-empty", authorArg, dateArg, "-m", newMessage)
	commitCmd.Dir = newRepoPath

	// Set GIT_COMMITTER_DATE to preserve the commit date as well
	commitCmd.Env = append(os.Environ(), fmt.Sprintf("GIT_COMMITTER_DATE=%d", committerWhen))

	ui.LogShellCommand("git", []string{"commit", "--allow-empty", authorArg, dateArg, "-m", newMessage}, newRepoPath)

	if output, err := commitCmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "nothing to commit") {
			ui.LogInfo("No changes to commit for %s", commitID[:8])
			return nil
		}
		return fmt.Errorf("failed to commit to new repo: %v, output: %s", err, output)
	}

	return nil
}
