package read

import (
	"context"
	"errors"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrAmbiguous = errors.New("ambiguous source association")

type Matcher interface {
	ReadMatching(context.Context, Handle, string, string, int, []source.Filter) ([]source.SourceRow, error)
}

// OptionalOne distinguishes absence from read failure. Two matching records
// are an error; callers must not silently choose a course or arbitrary row.
func OptionalOne(ctx context.Context, r Matcher, h Handle, table, column, key string) (source.SourceRow, error) {
	if r == nil || h.Validate() != nil {
		return nil, ErrHandle
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := r.ReadMatching(ctx, h, table, "", 2, []source.Filter{{Column: column, Value: key}})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) > 1 {
		return nil, ErrAmbiguous
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if rows[0] == nil {
		return nil, errors.New("invalid source association row")
	}
	return rows[0], nil
}
