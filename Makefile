BIN := bin/phantomguard
VERSION ?= v0.1.3
LDFLAGS := -s -w -X github.com/phantomguard/phantomguard/pkg/buildinfo.Version=$(VERSION)

.PHONY: build test fmt check release release-local docker

build:
	mkdir -p bin
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/phantomguard

test:
	go test ./...

fmt:
	gofmt -w cmd pkg data

check:
	gofmt -d cmd pkg data
	go vet ./...
	go test ./...

release:
	@echo "Use 'git tag v<version> && git push --tags' to trigger GitHub Actions release with GoReleaser"
	@echo "GoReleaser is configured in .goreleaser.yml and automated via .github/workflows/release.yml"

release-local:
	go run ./scripts/release-package -version $(VERSION)

docker:
	docker build -t phantomguard:local .
