package meili

import "github.com/meilisearch/meilisearch-go"

type Request struct {
	Type  string
	Index string

	Data interface{}
}

type Response struct {
	Requests []*Request
	TaskInfo *meilisearch.TaskInfo
}
