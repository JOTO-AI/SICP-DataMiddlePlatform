package staging

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

// CopyTable verifies a complete full table before copying any page into a new
// run. Partial tables are never resumed or combined with fresh upstream pages.
// Other tables in the source run may be absent, incomplete or corrupt.
func (s *Store) CopyTable(ctx context.Context, table string, target *Store, mode ingest.Mode, limits ingest.PageLimits) (int64, error) {
	if !sourceKnown(table) || target == nil || target.runID == s.runID || (mode != ingest.Full && mode != ingest.ForceFull) {
		return 0, ErrInvalid
	}
	if err := s.notRetired(ctx); err != nil {
		return 0, err
	}
	entries, err := s.entries(ctx, s.runID+"."+table+".")
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, ErrIncomplete
	}
	scope := entries[0].Request
	scope.Page = 1
	if scope.Mode != ingest.Full && scope.Mode != ingest.ForceFull {
		return 0, ErrIncomplete
	}
	pager, err := ingest.NewPager(table, scope.PageSize, limits)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		r := entry.Request
		r.Page = 1
		if r != scope || r.Table != table {
			return 0, ErrCorrupt
		}
		page, err := s.readPage(ctx, entry)
		if err != nil {
			return 0, err
		}
		if err := pager.Accept(entry.Request.Page, page); err != nil {
			return 0, ErrIncomplete
		}
		m := pager.Manifest()
		if m.Pages[len(m.Pages)-1] != entry.Summary {
			return 0, ErrCorrupt
		}
	}
	if !pager.Manifest().Complete {
		return 0, ErrIncomplete
	}
	for _, entry := range entries {
		if err := s.notRetired(ctx); err != nil {
			return 0, err
		}
		page, err := s.readPage(ctx, entry)
		if err != nil {
			return 0, err
		}
		r := entry.Request
		r.Mode = mode
		if err := target.CommitPage(ctx, r, page, entry.Summary); err != nil {
			return 0, err
		}
	}
	return int64(len(entries)), nil
}
