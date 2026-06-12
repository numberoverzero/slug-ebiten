.PHONY: build

# golangci-lint is pinned in tools/go.mod, an isolated module, so the linter's
# large dependency graph never leaks into the main module's go.mod (which is
# what `go get` consumers fetch). We compile it from there into a gitignored
# local bin and run that against this module. `go build` is cached, so repeat
# runs -- including the pre-commit hook -- are fast after the first compile.
GOLANGCI_LINT := $(CURDIR)/bin/golangci-lint

build:
	@git config core.hooksPath .githooks
	@go -C tools build -o $(GOLANGCI_LINT) github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	$(GOLANGCI_LINT) run ./...
	go test ./...
	go build ./...
