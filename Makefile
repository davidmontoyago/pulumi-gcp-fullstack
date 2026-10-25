.PHONY: build clean test lint lint-native lint-fix

build: clean
	go build -o ./build/ ./...

test: build
	go test -v -race -count=1 -timeout=30s -coverprofile=coverage.out ./...

clean:
	go mod tidy
	go mod verify

GOLANGCI_LINT_VERSION := v2.13.2
# Go version in the golangci-lint image. golangci-lint can only read export data written by the
# Go releases it supports, and v2.13.2 can't read go1.27.2's.
GOLANGCI_LINT_GO := go1.27.0
LINT_ARGS ?=

# Pipeline: pinned image, isolated from the host toolchain.
# A cold build cache needs ~16 GiB of Docker memory to compile the Pulumi provider SDKs.
lint:
	docker run --rm -v $$(pwd):/app \
		-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
		-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
		-w /app golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) \
		golangci-lint run $(LINT_ARGS)

# Local dev: same version built natively, reusing the host's warm build cache.
lint-native:
	GOTOOLCHAIN=$(GOLANGCI_LINT_GO) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run $(LINT_ARGS)

lint-fix:
	$(MAKE) lint-native LINT_ARGS=--fix

upgrade:
	go get -u ./...
