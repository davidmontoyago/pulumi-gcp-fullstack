.PHONY: build clean test lint

build: clean
	go build -o ./build/ ./...

test: build
	go test -v -race -count=1 -timeout=30s -coverprofile=coverage.out ./...

clean:
	go mod tidy
	go mod verify

lint:
	docker run --rm -v $$(pwd):/app \
		-v $$(go env GOCACHE):/.cache/go-build -e GOCACHE=/.cache/go-build \
		-v $$(go env GOMODCACHE):/.cache/mod -e GOMODCACHE=/.cache/mod \
		-w /app golangci/golangci-lint:v2.4.0 \
		golangci-lint run --fix --verbose --output.text.colors

upgrade:
	go get -u ./...
