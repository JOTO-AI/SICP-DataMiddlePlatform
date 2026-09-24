package candidate

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
)

func TestT13_16FullDeletionAndIncrementalRetentionUseSameVerifiedBaseline(t *testing.T) {
	for _, mode := range []ingest.Mode{ingest.ForceFull, ingest.Incremental} {
		t.Run(string(mode), func(t *testing.T) {
			input, baseFiles, o, _ := targetInput(t, ingest.Full, `{"ID":"a"}`, `{"ID":"b"}`)
			base, err := baseFiles.Prepare(t.Context(), o, input)
			if err != nil {
				t.Fatal(err)
			}
			input, files, o, _ := targetInput(t, mode, `{"ID":"b"}`)
			o.Combination = "C2"
			o.Base = &base
			o.BaseSource = baseFiles
			o.Quality = map[string]merge.Quality{"dwd_bzkscjzlb": {MaximumDeletedRows: 1}}
			m, err := files.Prepare(t.Context(), o, input)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{`{"ID":"b"}`}
			if mode == ingest.Incremental {
				want = []string{`{"ID":"a"}`, `{"ID":"b"}`}
			}
			if got := targetObjects(t, files, m); !reflect.DeepEqual(got, want) {
				t.Fatal(got, want)
			}
			counts, err := files.Changes(t.Context(), m)
			if err != nil {
				t.Fatal(err)
			}
			c := counts["dwd_bzkscjzlb"]
			if c.Before != 2 || c.After != int64(len(want)) || c.Added != 0 || c.Modified != 0 || c.Removed != int64(2-len(want)) || c.DeletionsKnown != (mode == ingest.ForceFull) {
				t.Fatal(c)
			}
		})
	}
}
func TestT13_16AbnormalFullEmptyIsRejectedBeforeCompleteTarget(t *testing.T) {
	input, baseFiles, o, _ := targetInput(t, ingest.Full, `{"ID":"a"}`)
	base, err := baseFiles.Prepare(t.Context(), o, input)
	if err != nil {
		t.Fatal(err)
	}
	input, files, o, _ := targetInput(t, ingest.ForceFull)
	o.Combination = "C2"
	o.Base = &base
	o.BaseSource = baseFiles
	o.Quality = map[string]merge.Quality{"dwd_bzkscjzlb": {MaximumDeletedRows: 100}}
	m, err := files.Prepare(t.Context(), o, input)
	if !errors.Is(err, merge.ErrQuality) || m.Combination != "" {
		t.Fatal(m, err)
	}
}
