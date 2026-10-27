.PHONY: all test build vsix install clean

# Default: Run test suite
all: test

# Run compiler validation tests
test:
	@go test ./pkg/compiler/...
	@go test ./cmd/...

# Build the VS Code .vsix package
vsix:
	@cd editors/vscode && npx --yes @vscode/vsce package

# Install CLI globally to /usr/local/bin
install:
	@go install ./cmd/ubec

# Clean up temporary compilation artifacts
clean:
	@rm -f output.c .temp_* .test_* ube_app
	@echo "Cleaned temporary build artifacts."
