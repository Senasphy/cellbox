.PHONY: build test lint web test-integration test-security fuzz bench release-dry-run

build:
	CGO_ENABLED=0 go build -trimpath -o bin/cellbox ./cmd/cellbox

test:
	go test -race ./...

lint:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './web/node_modules/*'))"
	go vet ./...

test-integration test-security fuzz bench release-dry-run:
	@echo "$@ is added in the corresponding implementation phase"

web:
	@echo "Dashboard is added in Phase 3"
