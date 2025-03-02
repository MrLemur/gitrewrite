# GitRewrite 🪄

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/MrLemur/gitrewrite)](https://goreportcard.com/report/github.com/MrLemur/gitrewrite)
[![Licence: MIT](https://img.shields.io/badge/Licence-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/MrLemur/gitrewrite?include_prereleases)](https://github.com/MrLemur/gitrewrite/releases)

**Intelligently rewrite your Git history with AI-powered commit messages.**

![Screenshot of GitRewrite in action](./docs/images/main-screenshot.png)

</div>

## 🚀 Overview

GitRewrite transforms your repository's history by converting cryptic or minimal commit messages into meaningful, structured, conventional commits. Using AI, it analyzes each commit's code changes and creates descriptive messages that explain _what_ changed and _why_.

**Before GitRewrite:**

```
fix stuff
```

**After GitRewrite:**

```
fix: resolve race condition in database connection pooling (database)
chore: update Docker image to v21.3.1 (infrastructure)
```

## 💡 Motivation

Every developer has encountered (or created) repositories with unclear commit histories. GitRewrite was born from the frustration of maintaining a GitOps repository where many small changes accumulated over time with minimal or unhelpful commit messages.

When you're debugging an issue or trying to understand why a change was made months ago, commit messages like "update config" or "fix bug" are nearly useless. GitRewrite transforms these into a clean, structured history that documents your codebase's evolution properly.

By enforcing [Conventional Commits](https://www.conventionalcommits.org/) standards and detecting affected components, GitRewrite makes your Git history into a powerful documentation tool rather than a cryptic timeline.

## ✨ Features

- **AI-Powered Message Generation**: Analyzes diffs to create meaningful, context-aware commit messages
- **Conventional Commits Format**: Structures messages with type, description, and component
- **Interactive TUI**: Beautiful terminal interface with real-time progress tracking
- **Dry Run Mode**: Preview changes before applying them
- **Batch Rewrite**: Process and rewrite multiple commits at once
- **Filters**: Target only commits with minimal or unclear messages
- **Repository Safety**: Careful validation prevents accidental history corruption

## 📋 Requirements

- Go 1.23.4+
- Git
- [Ollama](https://ollama.ai/) with a large language model installed (default: qwen2.5:14b)

## 🔧 Installation

### Using Go

```bash
go install github.com/MrLemur/gitrewrite/cmd/gitrewrite@latest
```

### From Source

```bash
git clone https://github.com/MrLemur/gitrewrite.git
cd gitrewrite
make build
```

### Ollama Setup

1. Install Ollama from [ollama.ai](https://ollama.ai)
2. Pull the recommended model:
   ```bash
   ollama pull qwen2.5:14b
   ```
3. Ensure Ollama is running before using GitRewrite

## 📖 Usage

### Basic Usage

```bash
gitrewrite -repo=/path/to/repository
```

### Options

```
  -repo string
        Path to the git repository
  -max-length int
        Maximum length of commit messages to consider for rewriting (default: 10)
  -model string
        Ollama model to use for rewriting (default: "qwen2.5:14b")
  -temperature float
        Temperature for model generation (default: 0.1)
  -max-diff int 
        Maximum length of diff to send to the model (default: 2048)
  -dry-run
        Generate new commit messages but don't apply them
  -output string
        Custom path for dry run output file (default: repo-name-rewrite-changes.json) 
  -apply-changes string
        Path to JSON file with commit rewrite changes to apply directly
```

### Workflow Example

1. Run GitRewrite on your repository:
   ```bash
   gitrewrite -repo=/path/to/repo 
   ```
   
2. If not using dry-run mode, a confirmation dialog will appear:
   ```
   WARNING: This process is irreversible and will modify your git history.
   
   'No' is selected by default. Use Tab to select 'Yes' if you want to proceed.
   ```
   Review the number of commits to be processed and select 'Yes' to continue or 'No' (default) to cancel.
   
3. To first preview changes without applying them, use dry-run mode:
   ```bash
   gitrewrite -repo=/path/to/repo -dry-run
   ```
   This will generate a JSON file (default: repo-name-rewrite-changes.json) with the proposed commit message changes.
   
4. Review the generated JSON file and make any desired edits.

5. Apply the changes from the JSON file:
   ```bash 
   gitrewrite -repo=/path/to/repo -apply-changes=path/to/changes.json
   ```
   The same confirmation dialog as in step 2 will appear before applying the changes.

## ⚠️ Cautions and FAQ

### Rewriting History Implications

- **Always backup your repository** before making bulk history changes
- Rewriting history changes commit hashes, which can cause issues for collaborators
- For shared repositories, communicate with your team before using this tool

### Common Questions

**Q: How long will it take to process my repository?**  
A: Processing time depends on repository size, commit count, and your machine's specs. A rough estimate is 2-5 seconds per commit being rewritten.

**Q: Will this affect branches?**  
A: Yes. Rewriting commits will change their hashes, which can affect branches that build upon those commits.

**Q: What if I don't like some of the generated messages?**  
A: Use the dry-run mode to preview changes, edit the JSON file as needed, then apply with `-apply-changes`.

**Q: Can I process only specific commits?**  
A: Currently, the tool processes all commits with messages shorter than the `-max-length` threshold.

## 🛠️ Development

```bash
# Clone the repository
git clone https://github.com/MrLemur/gitrewrite.git

# Install dependencies
cd gitrewrite
go mod tidy

# Run tests
make test

# Build
make build

# Run locally
./bin/gitrewrite -repo=/path/to/test/repo
```
