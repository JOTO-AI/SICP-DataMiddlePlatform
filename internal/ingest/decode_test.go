package ingest_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

func TestT03_01CatalogPathsKeysAndColumnsMatchIndependentPlan(t *testing.T) {
	keys := map[string]string{"dwd_xqjbsjzlb": "XQH", "dwd_yxsdwjbsjzlb": "DWH", "dwd_jzgjcsjzlb": "JGH", "dwd_grtxfszlb": "JGH", "dwd_bzksjbsjzlb": "XH", "dwd_bzksgrtxsjzlb": "XH", "dwd_bzksxjjbsjzlb": "XH", "dwd_bzkszyxxsjzlb": "ZYH", "dwd_bzksnjzysjzlb": "NJZYDM", "dwd_bzksbjsjzlb": "BH", "dwd_bzkskcsjzlb": "KCH", "dwd_bzkscjzlb": "ID", "dwd_bzkspksjzlb": "PKH", "dwd_bzkskbsjzlb": "ID"}
	data, err := os.ReadFile("../../mock_data/requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var requests []struct{ Table, Path string }
	if err := json.Unmarshal(data, &requests); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, request := range requests {
		paths[request.Table] = request.Path
	}
	data, err = os.ReadFile("../storage/migrate/testdata/expected_columns.txt")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		p := strings.Split(line, "|")
		expected[p[0]+"."+p[1]] = p[2]
	}
	counts := map[value.Kind]int{}
	history := map[string]bool{}
	catalog := ingest.Catalog()
	if len(catalog) != 14 {
		t.Fatal("missing sources")
	}
	for _, d := range catalog {
		if keys[d.Name] != d.PrimaryKey || paths[d.Name] != d.Path {
			t.Fatalf("wrong key/path: %s", d.Name)
		}
		if d.History {
			history[d.Name] = true
		}
		for _, column := range d.Columns {
			key := d.Name + "." + column.Name
			if expected[key] != string(column.Kind) {
				t.Fatal("wrong column", key)
			}
			delete(expected, key)
			counts[column.Kind]++
		}
	}
	if len(expected) != 0 || counts[value.Text] != 256 || counts[value.JSON] != 15 || counts[value.Timestamp] != 3 {
		t.Fatal("wrong source field totals", counts)
	}
	if len(history) != 3 || !history["dwd_bzksxjjbsjzlb"] || !history["dwd_bzksgrtxsjzlb"] || !history["dwd_grtxfszlb"] {
		t.Fatal("history merge exception applied to wrong tables")
	}
	catalog[0].Columns[0].Name = "MUTATED"
	if ingest.Catalog()[0].Columns[0].Name == "MUTATED" {
		t.Fatal("mutable source authority")
	}
}

func TestT03_02NumberTokensAndTypesRemainDistinct(t *testing.T) {
	for _, tc := range []struct {
		token string
		kind  ingest.JSONType
	}{{"3", ingest.Number}, {"3.0", ingest.Number}, {"900719925474099312345.0000000000001", ingest.Number}, {`"3"`, ingest.String}} {
		r, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(`{"ID":"0001","KCCJ":`+tc.token+`}`))
		if err != nil || r.Key != "0001" || r.Fields["KCCJ"].Type != tc.kind || string(r.Fields["KCCJ"].Raw) != tc.token {
			t.Fatal("token/type was changed", err)
		}
		row := r.SourceRow()
		row["KCCJ"][0] = 'x'
		if string(r.Fields["KCCJ"].Raw) != tc.token {
			t.Fatal("stored row shares mutable token")
		}
	}
}

func TestT03_03MissingNullEmptyAndLiteralNullStayDistinct(t *testing.T) {
	r, err := ingest.DecodeRecord("dwd_bzksjbsjzlb", []byte(`{"XH":"0001","XM":null,"XMPY":"","XBM":"null"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Fields["ETL_TIME"]; ok {
		t.Fatal("missing field invented")
	}
	if r.Fields["XM"].Type != ingest.Null || string(r.Fields["XM"].Raw) != "null" || string(r.Fields["XMPY"].Raw) != `""` || string(r.Fields["XBM"].Raw) != `"null"` {
		t.Fatal("presence/null distinctions collapsed")
	}
	row := r.SourceRow()
	if _, ok := row["ETL_TIME"]; ok || row["XM"] == nil {
		t.Fatal("conversion lost presence")
	}
}

func TestT03_04InvalidPrimaryKeysAreRejected(t *testing.T) {
	for _, input := range []string{`{}`, `{"ID":null}`, `{"ID":""}`, `{"ID":"  "}`, `{"ID":[]}`, `{"ID":123}`} {
		_, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(input))
		var validation *ingest.ValidationError
		if !errors.As(err, &validation) || validation.Kind != ingest.InvalidKey {
			t.Fatal("invalid primary key accepted", input, err)
		}
	}
}

func TestT03_05JSONTypeDifferencesDoNotChangeSchema(t *testing.T) {
	for _, tc := range []struct {
		token string
		kind  ingest.JSONType
	}{{`"优"`, ingest.String}, {"true", ingest.Boolean}, {"{}", ingest.Object}, {"[]", ingest.Array}, {"null", ingest.Null}} {
		r, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(`{"ID":"1","KCCJ":`+tc.token+`}`))
		if err != nil || string(r.Fields["KCCJ"].Raw) != tc.token || len(r.Differences) != 1 || r.Differences[0].Column != "KCCJ" || r.Differences[0].Expected != ingest.Number || r.Differences[0].Actual != tc.kind {
			t.Fatal("legal JSON type lost or not recorded", err)
		}
	}
	for _, input := range []string{`{"XQH":"01","XQMC":3}`, `{"XQH":"01","ETL_TIME":123}`, `{"XQH":"01","ETL_TIME":"2024-01-01T00:00:00.1234567"}`} {
		_, err := ingest.DecodeRecord("dwd_xqjbsjzlb", []byte(input))
		var validation *ingest.ValidationError
		if !errors.As(err, &validation) || validation.Kind != ingest.InvalidType {
			t.Fatal("TEXT/time schema silently changed", err)
		}
	}
}

func TestT03_05AmbiguousOrMalformedRecordsFailWithoutValues(t *testing.T) {
	for _, input := range []string{`{"ID":"private-value","ID":"other"}`, `{"ID":"1"} {}`, `[]`, `null`, `{"ID":"1","KCCJ":NaN}`, string([]byte{'{', '"', 'I', 'D', '"', ':', '"', 0xff, '"', '}'})} {
		if _, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(input)); err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatal("ambiguous record accepted or value leaked")
		}
	}
	if _, err := ingest.DecodeRecord("unknown", []byte(`{}`)); err == nil {
		t.Fatal("unknown source accepted")
	}
	_, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(strings.Repeat(" ", ingest.MaxRecordBytes+1)))
	var validation *ingest.ValidationError
	if !errors.As(err, &validation) || validation.Kind != ingest.RecordLimit {
		t.Fatal("record byte budget not enforced")
	}
}
