package read_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type matcherFunc func(context.Context, read.Handle, string, string, int, []source.Filter) ([]source.SourceRow, error)

func (f matcherFunc) ReadMatching(c context.Context, h read.Handle, t, a string, n int, p []source.Filter) ([]source.SourceRow, error) {
	return f(c, h, t, a, n, p)
}

func TestT06_11OptionalAssociationIsNotReadFailure(t *testing.T) {
	row := source.SourceRow{"KCH": json.RawMessage(`"001"`)}
	for _, kind := range []string{"absent", "present", "failure", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			failure := errors.New("storage failed")
			got, err := read.OptionalOne(t.Context(), matcherFunc(func(_ context.Context, h read.Handle, table, after string, n int, f []source.Filter) ([]source.SourceRow, error) {
				if h != original || table != "courses" || after != "" || n != 2 || len(f) != 1 || f[0].Column != "KCH" || f[0].Value != "001" || f[0].Contains {
					t.Fatal(h, table, after, n, f)
				}
				switch kind {
				case "absent":
					return nil, nil
				case "present":
					return []source.SourceRow{row}, nil
				case "duplicate":
					return []source.SourceRow{row, row}, nil
				default:
					return []source.SourceRow{row}, failure
				}
			}), original, "courses", "KCH", "001")
			switch kind {
			case "absent":
				if got != nil || err != nil {
					t.Fatal(got, err)
				}
			case "present":
				if string(got["KCH"]) != `"001"` || err != nil {
					t.Fatal(got, err)
				}
			case "duplicate":
				if got != nil || !errors.Is(err, read.ErrAmbiguous) {
					t.Fatal(got, err)
				}
			default:
				if got != nil || !errors.Is(err, failure) {
					t.Fatal(got, err)
				}
			}
		})
	}
}

func TestT06_12LaterAssociationFailureDiscardsWholeUseCase(t *testing.T) {
	for _, kind := range []string{"error", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			l := &lease{h: original}
			matcher := matcherFunc(func(context.Context, read.Handle, string, string, int, []source.Filter) ([]source.SourceRow, error) {
				calls++
				if calls == 2 {
					if kind == "cancel" {
						cancel()
						return []source.SourceRow{{}}, nil
					}
					return nil, errors.New("second association failed")
				}
				return []source.SourceRow{{"XH": json.RawMessage(`"001"`)}}, nil
			})
			got, err := read.Execute(ctx, admissionFunc(func(context.Context, string) (read.RequestLease, error) { return l, nil }), "", func(ctx context.Context, h read.Handle) (any, error) {
				first, err := read.OptionalOne(ctx, matcher, h, "students", "XH", "001")
				if err != nil {
					return nil, err
				}
				_, err = read.OptionalOne(ctx, matcher, h, "courses", "KCH", "001")
				return first, err
			})
			if err == nil || got != nil || calls != 2 || l.closed != 1 {
				t.Fatal(got, err, calls, l.closed)
			}
		})
	}
}
