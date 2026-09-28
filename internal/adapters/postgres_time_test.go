package adapters

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestT15_04PostgresTimesMatchJSONCheckpointRepresentation(t *testing.T) {
	for _, oid := range []uint32{pgtype.TimestamptzOID, pgtype.TimestampOID} {
		for _, format := range []int16{pgtype.BinaryFormatCode, pgtype.TextFormatCode} {
			types := pgtype.NewMap()
			configureTimeCodecs(types)
			want := time.Date(2026, 8, 31, 23, 59, 59, 999999000, time.UTC)
			wire, err := types.Encode(oid, format, want, nil)
			if err != nil {
				t.Fatal(err)
			}
			var scanned time.Time
			if err := types.Scan(oid, format, wire, &scanned); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(scanned)
			if err != nil {
				t.Fatal(err)
			}
			var checkpoint time.Time
			if err := json.Unmarshal(encoded, &checkpoint); err != nil {
				t.Fatal(err)
			}
			if scanned.Location() != time.UTC || !scanned.Equal(want) || !reflect.DeepEqual(scanned, checkpoint) {
				t.Fatalf("oid=%d format=%d SQL time and JSON checkpoint differ", oid, format)
			}
		}
	}
}
