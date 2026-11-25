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
	CELLBOX_RUN_DOCKER_EGRESS_INTEGRATION=1 go test -race ./internal/runtime/docker -run '^TestDockerAllowlistedHTTPSUsesEgressProxy$$' -count=1

test-security: build
	CGO_ENABLED=0 go test -c -tags security -o /tmp/cellbox-security.test ./test/security
	systemd-run --user --scope -p Delegate=yes /tmp/cellbox-security.test -test.v -test.count=1

fuzz:
	go test -fuzz=FuzzValidatePath -fuzztime=5s ./internal/sandboxpath
	go test -fuzz=FuzzValidateNativeArchive -fuzztime=5s ./internal/runtime/native
	go test -fuzz=FuzzDecodeControlPacket -fuzztime=5s ./internal/runtime/native
	go test -fuzz=FuzzProxyTargetAndHost -fuzztime=5s ./internal/egress
	go test -fuzz=FuzzOutputAndPagination -fuzztime=5s ./internal/api

release-dry-run:
	goreleaser check
	goreleaser release --snapshot --clean --skip=sign

bench:
	python3 scripts/benchmark-create.py

web:
	npm ci --prefix web
	npm run build --prefix web
	python3 scripts/embed_dashboard.py
