all: build

build: build-meilisearch

build-meilisearch:
	CGO_ENABLED=0 go build -o bin/go-mysql-meilisearch ./cmd/go-mysql-meilisearch

test:
	go test -timeout 1m --race ./...

clean:
	go clean -i ./...
	@rm -rf bin