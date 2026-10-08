package adapters

import (
	"bytes"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestBT05_BinaryCopyCodecsPreserveJSONAndNaiveTimestamp(t *testing.T) {
	types := pgtype.NewMap()
	configureTimeCodecs(types)
	for _, token := range []string{"3", "3.0", `"3"`, "null", "9007199254740993"} {
		raw, err := types.Encode(pgtype.JSONOID, pgtype.BinaryFormatCode, token, nil)
		if err != nil || !bytes.Equal(raw, []byte(token)) {
			t.Fatal("JSON rewritten for COPY", err)
		}
	}
	at := time.Date(2026, 9, 1, 12, 34, 56, 123456000, time.UTC)
	raw, err := types.Encode(pgtype.TimestampOID, pgtype.BinaryFormatCode, at, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got time.Time
	if err = types.Scan(pgtype.TimestampOID, pgtype.BinaryFormatCode, raw, &got); err != nil || !got.Equal(at) {
		t.Fatal("naive timestamp changed", err)
	}
}
