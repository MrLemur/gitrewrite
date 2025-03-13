package commands

import (
	"flag"
)

var (
	// Command line flags
	RepoPath                  string
	MaxMsgLength              int
	Model                     string
	Temperature               float64
	MaxDiffLength             int
	DryRun                    bool
	OutputFile                string
	ApplyChangesFile          string
	ExcludeFiles              string
	MaxFilesPerCommit         int
	SummarizeOversizedCommits bool
	DebugLogFile              string
)

// ParseFlags parses command line flags
func ParseFlags() {
	flag.StringVar(&RepoPath, "repo", "", "Path to the git repository")
	flag.IntVar(&MaxMsgLength, "max-length", 10, "Maximum length of commit messages to consider for rewriting")
	flag.StringVar(&Model, "model", "qwen2.5:14b", "Ollama model to use for rewriting")
	flag.Float64Var(&Temperature, "temperature", 0.1, "Temperature for model generation (0.0-1.0)")
	flag.IntVar(&MaxDiffLength, "max-diff", 2048, "Maximum length of diff to send to the model")
	flag.BoolVar(&DryRun, "dry-run", false, "Generate new commit messages but don't apply them")
	flag.StringVar(&OutputFile, "output", "", "Custom path for dry run output file (default: repo-name-rewrite-changes.json)")
	flag.StringVar(&ApplyChangesFile, "apply-changes", "", "Path to JSON file with commit rewrite changes to apply directly without using Ollama")
	flag.StringVar(&ExcludeFiles, "exclude", "", "Regex pattern to exclude matching files from diff processing")
	flag.IntVar(&MaxFilesPerCommit, "max-files", 200, "Maximum number of files in a commit before handling differently")
	flag.BoolVar(&SummarizeOversizedCommits, "summarize-oversized", false, "Generate a one-line summary for commits with too many files instead of skipping them")
	flag.StringVar(&DebugLogFile, "debug-log", "", "Path to output debug log file")
	flag.Parse()
}
