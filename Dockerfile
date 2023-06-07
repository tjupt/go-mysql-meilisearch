FROM golang:alpine

MAINTAINER tongyifan

RUN apk add --no-cache tini mariadb-client

ADD . /go/src/github.com/tjupt/go-mysql-meilisearch

RUN apk add --no-cache mariadb-client
RUN cd /go/src/github.com/tjupt/go-mysql-meilisearch/ && \
    go build -o bin/go-mysql-meilisearch ./cmd/go-mysql-meilisearch && \
    cp -f ./bin/go-mysql-meilisearch /go/bin/go-mysql-meilisearch

ENTRYPOINT ["/sbin/tini","--","go-mysql-meilisearch"]
