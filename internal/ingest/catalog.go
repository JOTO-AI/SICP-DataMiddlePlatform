// Package ingest validates and collects the fourteen explicitly configured sources.
package ingest

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const SourcePrefix = "/apizt/openapi/invoke/sjzc/release/"

type Definition struct {
	Name, Path, PrimaryKey string
	Columns                []source.Column
	History                bool
}

var definitions = func() map[string]Definition {
	result := map[string]Definition{}
	for _, table := range source.Catalog() {
		history := table.Name == "dwd_bzksxjjbsjzlb" || table.Name == "dwd_bzksgrtxsjzlb" || table.Name == "dwd_grtxfszlb"
		result[table.Name] = Definition{Name: table.Name, Path: SourcePrefix + table.Name, PrimaryKey: table.PrimaryKey, Columns: table.Columns, History: history}
	}
	return result
}()

func Catalog() []Definition {
	result := make([]Definition, 0, len(definitions))
	for _, table := range source.Catalog() {
		d := definitions[table.Name]
		d.Columns = append([]source.Column(nil), d.Columns...)
		result = append(result, d)
	}
	return result
}
