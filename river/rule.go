package river

import (
	"github.com/meilisearch/meilisearch-go"
	"strings"

	"github.com/go-mysql-org/go-mysql/schema"
)

// Rule is the rule for how to sync data from MySQL to meilisearch.
// If you want to sync MySQL data into meilisearch, you must set a rule to let use know how to do it.
// The mapping rule may thi: schema + table <-> index + document type.
// schema and table is for MySQL, index and document type is for meilisearch.
type Rule struct {
	Schema string   `toml:"schema"`
	Table  string   `toml:"table"`
	Index  string   `toml:"index"`
	ID     []string `toml:"id"` // todo: support

	// Default, a MySQL table field name is mapped to meilisearch field name.
	// Sometimes, you want to use different name, e.g, the MySQL file name is title,
	// but in meilisearch, you want to name it my_title.
	FieldMapping map[string]string `toml:"field"`

	// MySQL table information
	TableInfo *schema.Table

	//only MySQL fields in filter will be synced , default sync all fields
	Filter []string `toml:"filter"`

	IndexSettings *meilisearch.Settings `toml:"meili"`
}

func newDefaultRule(schema string, table string) *Rule {
	r := new(Rule)

	r.Schema = schema
	r.Table = table

	lowerTable := strings.ToLower(table)
	r.Index = lowerTable

	r.FieldMapping = make(map[string]string)

	return r
}

func (r *Rule) prepare() error {
	if r.FieldMapping == nil {
		r.FieldMapping = make(map[string]string)
	}

	if len(r.Index) == 0 {
		r.Index = r.Table
	}

	return nil
}

// CheckFilter checkers whether the field needs to be filtered.
func (r *Rule) CheckFilter(field string) bool {
	if r.Filter == nil {
		return true
	}

	for _, f := range r.Filter {
		if f == field {
			return true
		}
	}
	return false
}
