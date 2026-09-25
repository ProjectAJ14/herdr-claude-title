BINARY := herdr-claude-title
PKG    := ./cmd/herdr-claude-title
LINT   := go tool -modfile=tools/go.mod golangci-lint

.DEFAULT_GOAL := help

.PHONY: help
help: ## List every target
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-13s\033[0m %s\n", $$1, $$2}'

.PHONY: check
check: fmt vet lint test ## The gate before every commit

.PHONY: fmt
fmt: ## Format and apply every automatic lint fix
	@$(LINT) fmt ./...
	@$(LINT) run --fix ./... >/dev/null || true

.PHONY: vet
vet: ## go vet, for this platform and for Windows
	@go vet ./...
	@GOOS=windows go vet ./...

.PHONY: lint
lint: ## golangci-lint, pinned in tools/go.mod
	@$(LINT) run ./...

.PHONY: test
test: ## Tests with the race detector
	@go test -race ./...

.PHONY: live-claude
live-claude: ## Spend one real Haiku call to prove claude -p works here
	@LIVE_CLAUDE=1 go test -run TestLiveClaude -v ./internal/summarizer/

.PHONY: build
build: ## Build the binary
	@go build -o $(BINARY) $(PKG)

.PHONY: run
run: build ## Run in the current Herdr session, debug logging (takes over from the installed one)
	@HERDR_CLAUDE_TITLE_LOG_LEVEL=debug ./$(BINARY)

.PHONY: ps
ps: ## Show running instances
	@pgrep -fl '/$(BINARY)$$' || echo "nothing running"

.PHONY: stop
stop: ## Stop every running instance
	@pkill -f '/$(BINARY)$$' 2>/dev/null && echo "stopped" || echo "nothing running"

.PHONY: tabs
tabs: ## Current tab and pane labels
	@./scripts/probe.py tabs

.PHONY: snapshot
snapshot: ## The raw session snapshot the plugin polls
	@./scripts/probe.py snapshot

.PHONY: clean
clean: ## Remove the built binary
	@rm -f $(BINARY) $(BINARY).exe
