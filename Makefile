.PHONY: build run test clean docs

# Build variables
BINARY_NAME=gitrewrite
GO_FILES=$(shell find . -name '*.go' -not -path "./vendor/*")
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Default target
all: build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

# Run the application
run:
	@go run ./cmd/$(BINARY_NAME)

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean

# Generate documentation
docs:
	@echo "Generating documentation..."
	@godoc -http=:6060 &
	@echo "Documentation server started at http://localhost:6060"

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -s -w $(GO_FILES)

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod tidy

# Build for multiple platforms
build-all: clean
	@echo "Building for multiple platforms..."
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/$(BINARY_NAME)
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/$(BINARY_NAME)
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/$(BINARY_NAME)
	@echo "Binaries built in bin/ directory"
