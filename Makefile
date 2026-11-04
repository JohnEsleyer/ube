.PHONY: all test build bin vsix install clean

GO      ?= go
BIN_DIR ?= bin

# Default: run the test suite
all: test

# Run compiler and CLI tests
test:
	@$(GO) test ./pkg/compiler/...
	@$(GO) test ./cmd/...

# Build into the working tree (for `go run`-style iteration)
build:
	@$(GO) build ./cmd/ube
	@if [ -d ./cmd/ubec ]; then $(GO) build ./cmd/ubec; fi

# Build into ./bin for distribution
bin: $(BIN_DIR)/ube
	@if [ -d ./cmd/ubec ]; then $(GO) build -o $(BIN_DIR)/ubec ./cmd/ubec; fi
	@echo "Built binaries in ./$(BIN_DIR)"

$(BIN_DIR)/ube:
	@mkdir -p $(BIN_DIR)
	@$(GO) build -o $(BIN_DIR)/ube ./cmd/ube

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