BINARY := bin/herdr-panel
PKG    := ./cmd/herdr-panel
TOOL   := go tool -modfile=tools/go.mod

.DEFAULT_GOAL := help

.PHONY: help
help: ## List every target
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk -F':.*?## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: check
check: fmt-check vet lint test licenses ## Everything that must be green before a commit

.PHONY: fmt
fmt: ## Format the code
	@$(TOOL) golangci-lint fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail when a file is not formatted
	@$(TOOL) golangci-lint fmt --diff ./...

.PHONY: vet
vet: ## Static checks
	@go vet ./...

# Linux and macOS are both supported; files built for one only are linted for
# both. GOOS on `go tool` would build the linter for that platform, so the
# binary built for this machine is run with GOOS set instead.
.PHONY: lint
lint: ## golangci-lint, pinned in tools/go.mod, for linux and darwin
	@GOOS=linux "$$($(TOOL) -n golangci-lint)" run ./...
	@GOOS=darwin "$$($(TOOL) -n golangci-lint)" run ./...

.PHONY: test
test: ## Tests with the race detector
	@go test -race ./...

.PHONY: fuzz
fuzz: ## Run each fuzz target for 30 s
	@for t in $$(go test -list '^Fuzz' ./... | grep '^Fuzz'); do \
		pkg=$$(grep -rl "func $$t(" --include='*_test.go' . | head -1 | xargs dirname); \
		go test -run '^$$' -fuzz "^$$t$$" -fuzztime 30s "$$pkg" || exit 1; \
	done

.PHONY: licenses
licenses: ## Every linked module is in the README's third-party table
	@./scripts/check-third-party

.PHONY: vuln
vuln: ## govulncheck
	@$(TOOL) govulncheck ./...

.PHONY: gorelease
gorelease: ## API compatibility against the latest tag
	@$(TOOL) gorelease

.PHONY: bench
bench: ## Startup benchmark
	@go test -run '^$$' -bench . -benchmem ./...

.PHONY: integration
integration: ## The herdr integration suite, against an isolated herdr
	@cd integration && go test -count=1 ./...

.PHONY: build
build: ## Build the binary
	@go build -o $(BINARY) $(PKG)

.PHONY: clean
clean: ## Remove the built binary
	@rm -rf bin
