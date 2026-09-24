package update

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func resultCounts(in PreparedUpdate, reports [2]candidate.Report) (control.Document, error) {
	if len(in.Changes) != len(in.Manifest.Tables) {
		return "", control.ErrEntity
	}
	for _, v := range in.Manifest.Tables {
		c, ok := in.Changes[v.Table]
		if !ok || c.After != v.Rows {
			return "", control.ErrEntity
		}
	}
	s := control.ResultSummary{Sources: map[string]control.SourceCounts{}}
	for name, c := range in.Changes {
		s.Sources[name] = control.SourceCounts{Before: c.Before, After: c.After, Added: c.Added, Modified: c.Modified, Removed: c.Removed, DeletionsKnown: c.DeletionsKnown}
	}
	for n, r := range reports {
		s.Copies[n] = control.CopyCounts{WrittenRows: r.WrittenRows, RebuiltTables: len(r.Rebuilt), ReusedTables: len(r.Reused)}
	}
	return control.SummaryDocument(s)
}
