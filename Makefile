GOLANGCI_LINT_VERSION ?= v2.12.2
ACTIONLINT_VERSION ?= v1.7.12

GO_FILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: format fmt format-check lint vet test build actionlint yaml-lint check tools

format fmt:
	gofmt -w $(GO_FILES)

format-check:
	@test -z "$$(gofmt -l $(GO_FILES))" || { \
		echo "The following Go files are not formatted:"; \
		gofmt -l $(GO_FILES); \
		exit 1; \
	}

lint:
	golangci-lint run ./...

vet:
	go vet ./...

test:
	go test ./...

build:
	go build ./...

actionlint:
	actionlint

yaml-lint:
	yamllint .github

check: format-check vet lint test build actionlint yaml-lint

tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
