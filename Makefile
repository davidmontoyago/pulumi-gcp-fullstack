.PHONY: build clean test lint

build:
	go build -o ./build/ ./...

test: build
	go test -v -race -count=1 -timeout=30s ./...

clean:
	go mod tidy

lint_local:
	docker run --rm -it -v $$(PWD):/app \
	-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
	-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
	-w /app golangci/golangci-lint:latest \
	golangci-lint run --verbose --color=always
