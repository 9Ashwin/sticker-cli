VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD)
LINT_VERSION := v2.13.2

.PHONY: build test vet lint fmt-check build-cross e2e-agent quality
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/sticker ./cmd/sticker
test:
	go test ./...
vet:
	go vet ./...
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION) run ./...
fmt-check:
	@test -z "$$(gofmt -l $$(git ls-files '*.go') $$(git ls-files --others --exclude-standard '*.go'))"
build-cross:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/sticker-windows-amd64.exe ./cmd/sticker
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/sticker-linux-amd64 ./cmd/sticker
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/sticker-linux-arm64 ./cmd/sticker
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o bin/sticker-darwin-amd64 ./cmd/sticker
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/sticker-darwin-arm64 ./cmd/sticker
e2e-agent:
	GOTOOLCHAIN=auto ./scripts/agent-e2e.py
quality: fmt-check test vet lint build build-cross
