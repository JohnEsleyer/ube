.PHONY: all test build vsix install clean

# Default: Run test suite
all: test

# Run compiler validation tests
test:
	@go test ./pkg/compiler/...
	@go test ./cmd/...

# Build the VS Code .vsix package
vsix:
	@command -v vsce >/dev/null 2>&1 || { echo "vsce is required; install it with: npm install --global @vscode/vsce" >&2; exit 1; }
	@cd editors/vscode && vsce package --no-dependencies

# Install the Ube toolchain and configure the user's shell PATH
install:
	@./install.sh

# Build the unified CLI and legacy core compiler
build:
	@go build ./cmd/ube
	@go build ./cmd/ubec

# Clean up temporary compilation artifacts
clean:
	@rm -f output.c .temp_* .test_* ube_app
	@echo "Cleaned temporary build artifacts."
