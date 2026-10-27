.PHONY: all test build vsix install clean

# Default: Run test suite
all: test

# Run compiler validation tests
test:
	@./bin/ube test

# Build the VS Code .vsix package
vsix:
	@cd editors/vscode && python3 build_vsix.py

# Install CLI globally to /usr/local/bin
install:
	@chmod +x bin/ube
	@cp bin/ube /usr/local/bin/ube
	@echo "🍠 Ube CLI installed to /usr/local/bin/ube"

# Clean up temporary compilation artifacts
clean:
	@rm -f output.c .temp_* .test_* ube_app
	@echo "Cleaned temporary build artifacts."
