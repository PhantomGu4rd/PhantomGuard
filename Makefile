BIN := bin/phantomguard
VERSION ?= v0.1.3
LDFLAGS := -s -w -X github.com/phantomguard/phantomguard/pkg/buildinfo.Version=$(VERSION)

.PHONY: build test fmt check release docker

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
	go run ./scripts/release-package -version $(VERSION)

docker:
	docker build -t phantomguard:local .
