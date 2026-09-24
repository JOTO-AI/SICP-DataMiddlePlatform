package control

import (
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type SourceCounts struct {
	Before, After, Added, Modified, Removed int64
	DeletionsKnown                          bool
}
type CopyCounts struct {
	WrittenRows                 int64
	RebuiltTables, ReusedTables int
}
type ResultSummary struct {
	Sources map[string]SourceCounts
	Copies  [2]CopyCounts
}

func (s ResultSummary) Validate() error {
	if len(s.Sources) != len(source.Catalog()) {
		return ErrEntity
	}
	for _, table := range source.Catalog() {
		c, ok := s.Sources[table.Name]
		if !ok || c.Before < 0 || c.After < 0 || c.Added < 0 || c.Modified < 0 || c.Removed < 0 || c.Removed > c.Before || c.Added > c.After || c.Modified > c.After-c.Added || c.Before-c.Removed != c.After-c.Added || (!c.DeletionsKnown && c.Removed != 0) {
			return ErrEntity
		}
	}
	for _, c := range s.Copies {
		if c.WrittenRows < 0 || c.RebuiltTables < 0 || c.ReusedTables < 0 || c.RebuiltTables > 14 || c.ReusedTables > 14-c.RebuiltTables {
			return ErrEntity
		}
	}
	return nil
}
func SummaryDocument(s ResultSummary) (Document, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(s)
	return Document(raw), err
}
func validSummary(d Document) bool {
	if d == "" {
		return true
	} // Existing initialization checkpoints predate counts.
	if len(d) > 16<<10 {
		return false
	}
	var s ResultSummary
	return json.Unmarshal([]byte(d), &s) == nil && s.Validate() == nil
}
func optionalSummary(d Document) any {
	if d == "" {
		return nil
	}
	return string(d)
}
