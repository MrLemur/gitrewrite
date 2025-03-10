package commands

import (
	"flag"
)

var (
	// Command line flags
	RepoPath         string
	MaxMsgLength     int
	Model            string
	Temperature      float64
	MaxDiffLength    int
	DryRun           bool
	OutputFile       string
	ApplyChangesFile string
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
	flag.Parse()
}
