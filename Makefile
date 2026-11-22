.PHONY: build test lint web test-integration test-security fuzz bench release-dry-run

build:
	CGO_ENABLED=0 go build -trimpath -o bin/cellbox ./cmd/cellbox

test:
	go test -race ./...

lint:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './web/node_modules/*'))"
	go vet ./...

test-integration:
	CELLBOX_RUN_DOCKER_CONTRACT=1 go test -race ./internal/runtime/docker -run '^TestRuntimeContract$$' -count=1

test-security fuzz:
	@echo "$@ is added in the corresponding implementation phase"

release-dry-run:
	goreleaser check
	goreleaser release --snapshot --clean --skip=sign

bench:
	python3 scripts/benchmark-create.py

web:
	npm ci --prefix web
	npm run build --prefix web
	python3 scripts/embed_dashboard.py
