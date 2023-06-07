all: build

build: build-meilisearch

build-meilisearch:
	GO111MODULE=on go build -o bin/go-mysql-meilisearch ./cmd/go-mysql-meilisearch

test:
	GO111MODULE=on go test -timeout 1m --race ./...

clean:
	GO111MODULE=on go clean -i ./...
	@rm -rf bin