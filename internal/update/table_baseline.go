package update

import (
	"context"
	"io"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

// CopyBaselineTable carries untouched full tables from the verified published
// target, never from its possibly incremental original acquisition.
func CopyBaselineTable(ctx context.Context, source candidate.CompleteSource, manifest candidate.Manifest, request ingest.PageRequest, target *staging.Store, limits ingest.PageLimits) (int64, error) {
	for _, table := range manifest.Tables {
		if table.Table != request.Table {
			continue
		}
		rows, err := source.Open(ctx, manifest, table)
		if err != nil {
			return 0, err
		}
		defer rows.Close(context.WithoutCancel(ctx))
		pager, err := ingest.NewPager(table.Table, request.PageSize, limits)
		if err != nil {
			return 0, err
		}
		for number := 1; ; number++ {
			page := ingest.Page{Total: table.Rows, Records: []ingest.Record{}}
			for len(page.Records) < request.PageSize {
				row, err := rows.Next(ctx)
				if err == io.EOF {
					break
				}
				if err != nil {
					return 0, err
				}
				page.Records = append(page.Records, row)
			}
			if err := pager.Accept(number, page); err != nil {
				return 0, err
			}
			request.Page = number
			m := pager.Manifest()
			if err := target.CommitPage(ctx, request, page, m.Pages[len(m.Pages)-1]); err != nil {
				return 0, err
			}
			if m.Complete {
				if _, err := rows.Next(ctx); err != io.EOF {
					return 0, candidate.ErrBuild
				}
				return int64(number), nil
			}
		}
	}
	return 0, candidate.ErrBuild
}
