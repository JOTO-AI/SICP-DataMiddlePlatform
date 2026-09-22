package value_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

func field(raw string) value.Field { return value.Field{Present: true, Raw: json.RawMessage(raw)} }

func TestT02_03TextNullAndLiteralValues(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    value.Field
		expected any
	}{
		{"missing", value.Field{}, nil}, {"null", field("null"), nil}, {"empty", field(`""`), ""},
		{"literal_null", field(`"null"`), "null"}, {"leading_zero", field(`"000123"`), "000123"},
		{"whitespace", field(`"  x  "`), "  x  "}, {"unicode", field(`"\ud83d\ude00"`), "😀"},
		{"literal_escape", field(`"\\ud800"`), `\ud800`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := value.Bind(value.Text, tc.input)
			if err != nil || !reflect.DeepEqual(actual, tc.expected) {
				t.Fatalf("bind: %#v %v", actual, err)
			}
			raw, err := value.Restore(value.Text, actual)
			if err != nil {
				t.Fatal(err)
			}
			if actual == nil {
				if raw != nil {
					t.Fatal("SQL NULL became a literal")
				}
				return
			}
			var decoded string
			if json.Unmarshal(raw, &decoded) != nil || decoded != tc.expected {
				t.Fatal("text changed after restore")
			}
		})
	}
}

func TestT02_04JSONNullAndMissingRemainDistinct(t *testing.T) {
	missing, err := value.Bind(value.JSON, value.Field{})
	if err != nil || missing != nil {
		t.Fatal("missing JSON did not bind SQL NULL")
	}
	null, err := value.Bind(value.JSON, field("null"))
	if err != nil || null != "null" {
		t.Fatal("JSON null did not bind literal null")
	}
	for _, tc := range []struct {
		stored   any
		expected string
		sqlNull  bool
	}{{nil, "", true}, {"null", "null", false}, {`"null"`, `"null"`, false}, {`""`, `""`, false}} {
		raw, err := value.Restore(value.JSON, tc.stored)
		if err != nil || string(raw) != tc.expected || (raw == nil) != tc.sqlNull {
			t.Fatal("SQL NULL and JSON values collapsed")
		}
	}
}

func TestT02_05JSONTokensNeverUseFloat(t *testing.T) {
	for _, token := range []string{"3", "3.0", `"3"`, "9007199254740993123456789.0000000000001", "1.234567890123456789e+200", "-0", "0.0000000", `"优"`, `{"n":3.0}`, `[3,3.0,"3"]`} {
		parameter, err := value.Bind(value.JSON, field(token))
		if err != nil {
			t.Fatal(err)
		}
		text, ok := parameter.(string)
		if !ok || text != token {
			t.Fatalf("token altered: %s", token)
		}
		restored, err := value.Restore(value.JSON, parameter)
		if err != nil || string(restored) != token {
			t.Fatalf("restored token altered: %s", token)
		}
	}
}

func TestT02_05TimestampCalendarAndMicrosecondBoundary(t *testing.T) {
	for _, tc := range []struct {
		input, expected                               string
		year, month, day, hour, minute, second, nanos int
	}{
		{"2024-02-29 23:59:59.123456", "2024-02-29T23:59:59.123456", 2024, 2, 29, 23, 59, 59, 123456000},
		{"0001-01-01T00:00:00", "0001-01-01T00:00:00", 1, 1, 1, 0, 0, 0, 0},
		{"9999-12-31T23:59:59.999999", "9999-12-31T23:59:59.999999", 9999, 12, 31, 23, 59, 59, 999999000},
	} {
		parameter, err := value.Bind(value.Timestamp, field(`"`+tc.input+`"`))
		if err != nil {
			t.Fatal(err)
		}
		v, ok := parameter.(time.Time)
		if !ok || v.Year() != tc.year || int(v.Month()) != tc.month || v.Day() != tc.day || v.Hour() != tc.hour || v.Minute() != tc.minute || v.Second() != tc.second || v.Nanosecond() != tc.nanos || v.Location() != time.UTC {
			t.Fatal("timestamp calendar/precision changed")
		}
		// A driver's location tag must not shift TIMESTAMP WITHOUT TIME ZONE.
		v = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.FixedZone("synthetic", 8*3600))
		raw, err := value.Restore(value.Timestamp, v)
		if err != nil || string(raw) != `"`+tc.expected+`"` {
			t.Fatal("timestamp restored with a timezone shift")
		}
	}
	for _, input := range []string{"2023-02-29T00:00:00", "2024-01-01T24:00:00", "2024-01-01T00:00:60", "2024-01-01T00:00:00.1234567", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00+08:00", "0000-01-01T00:00:00", "2024-01-01"} {
		if _, err := value.Bind(value.Timestamp, field(`"`+input+`"`)); !errors.Is(err, value.ErrInvalid) {
			t.Fatalf("invalid timestamp accepted: %s", input)
		}
	}
	for _, input := range []value.Field{{}, field("null")} {
		if v, err := value.Bind(value.Timestamp, input); err != nil || v != nil {
			t.Fatal("timestamp null changed")
		}
	}
}

func TestT02_05RejectLossyAndMalformedBindings(t *testing.T) {
	for _, tc := range []struct {
		kind  value.Kind
		input value.Field
	}{
		{value.Text, field("3")}, {value.Text, field(`"\u0000"`)}, {value.Text, field(`"\ud800"`)}, {value.Text, field(`"\udc00"`)},
		{value.Text, field(`"\ud800\u1234"`)}, {value.Text, field(string([]byte{'"', 0xff, '"'}))},
		{value.JSON, field("3 trailing")}, {value.JSON, field("NaN")}, {value.JSON, field("01")},
		{value.JSON, field("")}, {value.JSON, value.Field{Raw: json.RawMessage("3")}}, {"unknown", value.Field{}},
	} {
		if _, err := value.Bind(tc.kind, tc.input); !errors.Is(err, value.ErrInvalid) {
			t.Fatal("invalid/lossy source value accepted")
		}
	}
	for _, tc := range []struct {
		kind   value.Kind
		stored any
	}{
		{value.JSON, float64(3)}, {value.JSON, "invalid"}, {value.Text, 123}, {value.Text, "a\x00b"},
		{value.Timestamp, time.Date(2024, 1, 1, 0, 0, 0, 1, time.UTC)}, {value.Timestamp, "2024-01-01"}, {"unknown", nil},
	} {
		if _, err := value.Restore(tc.kind, tc.stored); !errors.Is(err, value.ErrInvalid) {
			t.Fatal("lossy database value accepted")
		}
	}
}
