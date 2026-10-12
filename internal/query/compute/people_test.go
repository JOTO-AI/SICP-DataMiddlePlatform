package compute_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT17_08ExactIntegerAndCalendarConversions(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"3", 3}, {`"3"`, 3}, {"3.000", 3}, {`"3e0"`, 3}, {"100e-2", 1}, {"0.030e2", 3}, {"-2147483648", -2147483648}, {"2147483647", 2147483647}, {"-0", 0}, {"0e999999999999999999999999999", 0},
	} {
		got, err := compute.Integer32(source.SourceRow{"XZ": json.RawMessage(tc.raw)}, "XZ")
		if err != nil || got == nil || *got != tc.want {
			t.Fatal(tc.raw, got, err)
		}
	}
	for _, raw := range []string{"", "null", `""`, `"  "`} {
		got, err := compute.Integer32(source.SourceRow{"XZ": json.RawMessage(raw)}, "XZ")
		if got != nil || err != nil {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{"3.1", `"3/1"`, `"0x3"`, `"+3"`, `"03"`, `"3 "`, `"0e3 "`, "2147483648", "-2147483649", "9007199254740993", "3e99999999999999999999999", "1e-999999999999999999999", "1e-4", "true", "{}", "[]", `"NaN"`, `"Infinity"`} {
		got, err := compute.Integer32(source.SourceRow{"XZ": json.RawMessage(raw)}, "XZ")
		if got != nil || !errors.Is(err, compute.ErrValue) {
			t.Fatal("invalid integer accepted", raw, got, err)
		}
	}
	for _, tc := range []struct {
		name    string
		convert func(source.SourceRow, string) (*string, error)
		valid   map[string]string
		invalid []string
	}{
		{"month", compute.Month, map[string]string{"202609": "2026-09", "2026-09": "2026-09", "000101": "0001-01", "999912": "9999-12"}, []string{"000001", "202600", "202613", "2026-9", "2026/09", "2026-09-01", " 202609", "202609 ", "２０２６０９"}},
		{"date", compute.Date, map[string]string{"20240229": "2024-02-29", "2024-02-29": "2024-02-29", "20000229": "2000-02-29", "00010101": "0001-01-01"}, []string{"20260229", "19000229", "20260431", "2026-1-01", "2026/01/01", "2026-01-01T00:00:00", "00000101"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for input, want := range tc.valid {
				raw, _ := json.Marshal(input)
				got, err := tc.convert(source.SourceRow{"field": raw}, "field")
				if err != nil || got == nil || *got != want {
					t.Fatal(input, got, err)
				}
			}
			for _, input := range tc.invalid {
				raw, _ := json.Marshal(input)
				got, err := tc.convert(source.SourceRow{"field": raw}, "field")
				if got != nil || !errors.Is(err, compute.ErrValue) {
					t.Fatal(input, got, err)
				}
			}
			for _, raw := range []string{"", "null", `""`, `" \t "`} {
				got, err := tc.convert(source.SourceRow{"field": json.RawMessage(raw)}, "field")
				if got != nil || err != nil {
					t.Fatal(raw, got, err)
				}
			}
			if _, err := tc.convert(source.SourceRow{"field": json.RawMessage("202609")}, "field"); !errors.Is(err, compute.ErrValue) {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		code, want string
		lookup     func(*string) *string
	}{
		{"1", "教学院系", compute.OrganizationCategoryName}, {"5", "行政机构", compute.OrganizationCategoryName}, {"41", "专科生", compute.StudentCategoryName}, {"432", "中高贯通生", compute.StudentCategoryName},
	} {
		got := tc.lookup(&tc.code)
		if got == nil || *got != tc.want {
			t.Fatal(tc.code, got)
		}
	}
	for _, lookup := range []func(*string) *string{compute.OrganizationCategoryName, compute.StudentCategoryName} {
		if lookup(nil) != nil {
			t.Fatal("missing code mapped")
		}
		for _, code := range []string{"", "unknown", "01", " 1", " 41"} {
			if lookup(&code) != nil {
				t.Fatal("unknown code guessed", code)
			}
		}
	}
}
