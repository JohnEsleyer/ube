.PHONY: all test build vsix install clean

GO      ?= go
BIN_DIR ?= bin
TARGET  := $(BIN_DIR)/ube

# Default: run the test suite
all: test

# Run compiler and CLI tests
test:
	@$(GO) test ./pkg/compiler/...
	@$(GO) test ./cmd/...

# Build the compiler binary into ./bin/ube (always overwrites)
build:
	@mkdir -p $(BIN_DIR)
	@$(GO) build -o $(TARGET) ./cmd/ube
	@echo "Built and updated $(TARGET)"

# Package the VS Code extension
vsix:
	@command -v vsce >/dev/null 2>&1 || { echo "vsce is required; install it with: npm install --global @vscode/vsce" >&2; exit 1; }
	@cd editors/vscode && vsce package --no-dependencies

# Install the Ube toolchain and configure shell PATH
install:
	@./install.sh

# Remove build artifacts and the ./bin directory
clean:
	@rm -rf $(BIN_DIR)
	@rm -f output.c .temp_* .test_* ube_app
	@echo "Cleaned build artifacts."