package candidate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type validations struct {
	previous candidate.Evidence
	calls    []string
	fail     string
	ready    bool
}

func (v *validations) BeginValidation(context.Context, source.Target, candidate.Manifest) (candidate.Evidence, error) {
	v.calls = append(v.calls, "begin")
	v.ready = false
	return v.previous, nil
}
func (v *validations) Ready(_ context.Context, _ source.Target, _ candidate.Manifest, e candidate.Evidence) error {
	v.calls = append(v.calls, "ready")
	if v.fail == "commit" {
		return errors.New("commit failed")
	}
	v.ready = true
	v.previous = e
	return nil
}
func (v *validations) ValidationFailed(context.Context, source.Target) error {
	v.calls = append(v.calls, "failed")
	v.ready = false
	return nil
}
func (v *validations) SourceStorage(context.Context, read.Handle, candidate.Manifest) error {
	v.calls = append(v.calls, "source")
	if v.fail == "source" {
		return errors.New("source invalid")
	}
	return nil
}
func (v *validations) SourceIndexes(context.Context, read.Handle, candidate.Manifest) error {
	v.calls = append(v.calls, "indexes")
	if v.fail == "indexes" {
		return errors.New("indexes invalid")
	}
	return nil
}
func (v *validations) Check(_ context.Context, _ read.Handle, op contract.Operation, _ time.Time) (int, string, error) {
	v.calls = append(v.calls, string(op))
	if v.fail == string(op) {
		return 0, "", errors.New("query invalid")
	}
	return 1, strings.Repeat("a", 64), nil
}
func validator(v *validations) candidate.Validator {
	return candidate.Validator{Store: v, Checks: v, Clock: testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}}
}

func TestT12_04EveryValidationFailureKeepsCandidateUnready(t *testing.T) {
	_, target, m, _, _, _ := buildFixture(t)
	for _, failure := range []string{"source", "indexes", "commit"} {
		t.Run(failure, func(t *testing.T) {
			state := &validations{fail: failure, ready: true}
			got, err := validator(state).Validate(t.Context(), target, m)
			if err == nil || state.ready || got.Queries != nil || state.calls[len(state.calls)-1] != "failed" {
				t.Fatal(got, err, state.calls)
			}
			if failure == "source" && strings.Contains(strings.Join(state.calls, ","), "indexes") {
				t.Fatal(state.calls)
			}
			if failure == "indexes" && strings.Contains(strings.Join(state.calls, ","), ".query") {
				t.Fatal(state.calls)
			}
		})
	}
}
func TestT12_05EveryRoundRechecksStorageAndBindsAllTableVersions(t *testing.T) {
	_, target, m, _, _, _ := buildFixture(t)
	state := &validations{}
	v := validator(state)
	if _, err := v.Validate(t.Context(), target, m); err != nil {
		t.Fatal(err)
	}
	state.calls = nil
	if _, err := v.Validate(t.Context(), target, m); err != nil {
		t.Fatal(err)
	}
	if strings.Join(state.calls, ",") != "begin,source,indexes,ready" {
		t.Fatal("unchanged evidence not reused", state.calls)
	}
	for i := range m.Tables {
		if m.Tables[i].Table == grades.DepartmentsTable {
			m.Tables[i].SHA256 = strings.Repeat("b", 64)
		}
	}
	state.calls = nil
	if _, err := v.Validate(t.Context(), target, m); err != nil {
		t.Fatal(err)
	}
	if strings.Join(state.calls, ",") != "begin,source,indexes,ready" {
		t.Fatal("dimension dependency not respected", state.calls)
	}
	for _, kind := range []string{"generation", "combination"} {
		state.calls = nil
		switch kind {
		case "generation":
			target.Generation++
		case "combination":
			m.Combination = "C102"
		}
		if _, err := v.Validate(t.Context(), target, m); err != nil {
			t.Fatal(err)
		}
		if len(state.calls) != 4 {
			t.Fatal("stale evidence reused", kind, state.calls)
		}
	}
}

func TestLegacyCodeVersionDoesNotInvalidateEvidence(t *testing.T) {
	_, target, m, _, _, _ := buildFixture(t)
	state := &validations{}
	v := validator(state)
	evidence, err := v.Validate(t.Context(), target, m)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"CodeVersion":"previous-release",` + string(raw[1:])
	if err := json.Unmarshal([]byte(legacy), &state.previous); err != nil {
		t.Fatal(err)
	}
	if err := state.previous.Validate(target, m); err != nil {
		t.Fatal("legacy evidence rejected", err)
	}
	state.calls = nil
	result, err := v.Validate(t.Context(), target, m)
	if err != nil || strings.Join(state.calls, ",") != "begin,source,indexes,ready" {
		t.Fatal("legacy evidence not reused", err, state.calls)
	}
	raw, err = json.Marshal(result)
	if err != nil || strings.Contains(string(raw), "CodeVersion") {
		t.Fatal("new evidence contains code version", err, string(raw))
	}
}

type candidateSource struct {
	calls []string
	rows  map[string][]source.SourceRow
}

func (s *candidateSource) ReadMatching(_ context.Context, h read.Handle, table, after string, limit int, filters []source.Filter) ([]source.SourceRow, error) {
	if h.Combination != "C101" || h.Generation != 8 {
		return nil, read.ErrHandle
	}
	s.calls = append(s.calls, table)
	pk := ""
	for _, t := range source.Catalog() {
		if t.Name == table {
			pk = t.PrimaryKey
		}
	}
	var out []source.SourceRow
	for _, row := range s.rows[table] {
		id, err := compute.Required(row, pk)
		if err != nil {
			return nil, err
		}
		if id <= after {
			continue
		}
		match := true
		for _, f := range filters {
			value, _ := compute.Text(row, f.Column)
			if value == nil || *value != f.Value {
				match = false
			}
		}
		if match {
			out = append(out, row)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func onlineFixture(t *testing.T, s *candidateSource) (candidate.Online, read.Handle, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)
	clock := testkit.FakeClock{Time: at}
	times, _ := compute.NewTimes("Asia/Shanghai")
	_, target, _, _, _, _ := buildFixture(t)
	return candidate.Online{Source: s, Grades: grades.Service{Source: s, Times: times, Clock: clock, Limits: read.DefaultLimits()}, Timetable: timetable.Service{Source: s, Times: times, Clock: clock, Limits: read.DefaultLimits()}, Clock: clock, Limits: read.DefaultLimits()}, read.Handle{Position: target.Position, Generation: target.Generation, Combination: "C101"}, at
}
func TestT12_06ReadyChecksExecuteFourProductionServicesWithoutCacheOrDerivedWrites(t *testing.T) {
	s := &candidateSource{}
	online, h, at := onlineFixture(t, s)
	for _, d := range contract.LegacyBusinessDefinitions() {
		s.calls = nil
		count, hash, err := online.Check(t.Context(), h, d.Operation, at)
		if err != nil || count != 1 || len(hash) != 64 {
			t.Fatal(d.Operation, count, hash, err)
		}
		// One source scope scan followed by a second scan from the actual service.
		if len(s.calls) != 2 || s.calls[0] != s.calls[1] {
			t.Fatal(d.Operation, s.calls)
		}
	}
}
func TestT12_06RealCandidateDataDrivesOnlineConversionAndFailure(t *testing.T) {
	var row source.SourceRow
	if err := json.Unmarshal([]byte(`{"ID":"G1","XH":"001","XN":"2025-2026","XQM":"1","KCH":"C1","KCCJ":"pass"}`), &row); err != nil {
		t.Fatal(err)
	}
	s := &candidateSource{rows: map[string][]source.SourceRow{grades.GradesTable: {row}}}
	online, h, at := onlineFixture(t, s)
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades} {
		count, _, err := online.Check(t.Context(), h, op, at)
		want := 2
		if op == contract.StudentGrades {
			want = 3
		}
		if err != nil || count != want {
			t.Fatal(op, count, err)
		}
	}
	row["JD"] = json.RawMessage(`"invalid-number"`)
	if _, _, err := online.Check(t.Context(), h, contract.StudentGrades, at); err == nil {
		t.Fatal("real grade conversion bypassed")
	}
}

func TestBT07_QuerySessionPreservesSampleEvidence(t *testing.T) {
	var row source.SourceRow
	if err := json.Unmarshal([]byte(`{"ID":"G1","XH":"001","XN":"2025-2026","XQM":"1","KCH":"C1","KCCJ":"pass"}`), &row); err != nil {
		t.Fatal(err)
	}
	s := &candidateSource{rows: map[string][]source.SourceRow{grades.GradesTable: {row}}}
	o, h, at := onlineFixture(t, s)
	_, _, m, _, _, _ := buildFixture(t)
	queries, closeSession, err := candidate.QuerySession(t.Context(), o, h, m)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	for _, def := range contract.LegacyBusinessDefinitions() {
		count, digest, err := o.Check(t.Context(), h, def.Operation, at)
		if err != nil {
			t.Fatal(err)
		}
		got, summary, err := queries.Check(t.Context(), h, def.Operation, at)
		if err != nil || got != count || summary != digest {
			t.Fatal("memoized validation changed evidence", def.Operation, err)
		}
	}
}

func TestBT07GroupedStaffPreservesSampleOrderAndEvidence(t *testing.T) {
	ids := make([]string, 70)
	for i := range ids {
		ids[i] = fmt.Sprintf("T%03d", i)
	}
	var schedule source.SourceRow
	if err := json.Unmarshal([]byte(`{"PKH":"P1","JXBH":"J1","JHKCH":"C1","KKXND":"2026-2027","KKXQM":"1","SKRQ":"20260907","XQJ":"1","ZC":"1","SKJC":"1,2"}`), &schedule); err != nil {
		t.Fatal(err)
	}
	schedule["JSJGH"], _ = json.Marshal(strings.Join(ids, ","))
	s := &candidateSource{rows: map[string][]source.SourceRow{timetable.SchedulesTable: {schedule}}}
	o, h, at := onlineFixture(t, s)
	o.Limits = read.BackgroundLimits(time.Minute)
	o.Timetable.Limits = o.Limits
	n, digest, err := o.Check(t.Context(), h, contract.StaffTimetable, at)
	if err != nil || n != candidate.MaxBackgroundCases {
		t.Fatal(n, err)
	}
	before := len(s.calls)
	s.calls = nil
	o.GroupedStaff = true
	got, summary, err := o.Check(t.Context(), h, contract.StaffTimetable, at)
	if err != nil || got != n || summary != digest || len(s.calls) >= before {
		t.Fatal("grouped evidence differs or scans unchanged", got, err, len(s.calls), before)
	}
}

func TestBusinessSamplesIgnoreUnselectedIdentities(t *testing.T) {
	s := &candidateSource{rows: map[string][]source.SourceRow{}}
	for i := 0; i < 50; i++ {
		raw, _ := json.Marshal(map[string]string{"ID": fmt.Sprintf("G%03d", i), "XH": fmt.Sprintf("S%03d", i), "XN": "2025-2026", "XQM": "1", "KCH": "C1"})
		var row source.SourceRow
		_ = json.Unmarshal(raw, &row)
		if i == 49 {
			row["JD"] = json.RawMessage(`"invalid-number"`)
		}
		s.rows[grades.GradesTable] = append(s.rows[grades.GradesTable], row)
	}
	online, h, at := onlineFixture(t, s)
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades} {
		count, summary, err := online.Check(t.Context(), h, op, at)
		if err != nil || count < 2 || count > candidate.MaxBackgroundCases || len(summary) != 64 {
			t.Fatal("sample expanded into exhaustive validation", op, count, summary, err)
		}
	}
	// The same late malformed row must fail when it belongs to a selected
	// student, even though the response asks for only its first result page.
	s.rows[grades.GradesTable][49]["XH"] = json.RawMessage(`"S000"`)
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades} {
		count, summary, err := online.Check(t.Context(), h, op, at)
		if err == nil || count != 0 || summary != "" {
			t.Fatal("selected student's complete result was not validated", op, count, err)
		}
	}
}

func TestStorageEvidenceRequiresEveryTableAndNoBusinessQueries(t *testing.T) {
	_, target, m, _, _, _ := buildFixture(t)
	state := &validations{fail: string(contract.StaffTimetable)}
	evidence, err := validator(state).Validate(t.Context(), target, m)
	if err != nil || evidence.Policy != candidate.StorageCheckPolicy || len(evidence.Tables) != 14 || len(evidence.Queries) != 0 || strings.Join(state.calls, ",") != "begin,source,indexes,ready" {
		t.Fatal("business queries affected readiness", err, state.calls)
	}
	if evidence.Validate(target, m) != nil {
		t.Fatal("new evidence invalid")
	}
	for _, name := range []string{"missing", "count", "scope", "digest", "policy", "query"} {
		raw, _ := json.Marshal(evidence)
		var changed candidate.Evidence
		_ = json.Unmarshal(raw, &changed)
		table := m.Tables[0]
		switch name {
		case "missing":
			delete(changed.Tables, table.Table)
		case "count":
			table.Rows++
			changed.Tables[table.Table] = table
		case "scope":
			table.Scope = "other"
			changed.Tables[table.Table] = table
		case "digest":
			table.SHA256 = strings.Repeat("c", 64)
			changed.Tables[table.Table] = table
		case "policy":
			changed.Policy = "unknown"
		case "query":
			changed.Queries = map[contract.Operation]candidate.QueryEvidence{contract.StudentGrades: {Cases: 1}}
		}
		if changed.Validate(target, m) == nil {
			t.Fatal("accepted corrupted proof", name)
		}
	}
}

func TestT12_06StudentAndTeacherChecksExerciseActualTimetableParsing(t *testing.T) {
	decode := func(raw string) source.SourceRow {
		var row source.SourceRow
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	schedule := decode(`{"PKH":"P1","JXBH":"J1","JHKCH":"C1","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T1","SKRQ":"20260907","XQJ":"1","ZC":"1","SKJC":"1,2"}`)
	s := &candidateSource{rows: map[string][]source.SourceRow{
		timetable.RelationsTable: {decode(`{"ID":"R1","XH":"001","JXBH":"J1","JHKCH":"C1"}`)},
		timetable.SchedulesTable: {schedule},
	}}
	online, h, at := onlineFixture(t, s)
	for _, op := range []contract.Operation{contract.StudentTimetable, contract.StaffTimetable} {
		count, _, err := online.Check(t.Context(), h, op, at)
		if err != nil || count != 2 {
			t.Fatal(op, count, err)
		}
	}
	schedule["SKJC"] = json.RawMessage(`"invalid-section"`)
	for _, op := range []contract.Operation{contract.StudentTimetable, contract.StaffTimetable} {
		if _, _, err := online.Check(t.Context(), h, op, at); err == nil {
			t.Fatal("calendar conversion bypassed", op)
		}
	}
}

func TestFixedCasesQueryCurrentCandidateWithoutDiscovery(t *testing.T) {
	s := &candidateSource{rows: map[string][]source.SourceRow{}}
	// An unrelated record must never enter a fixed request's discovery path.
	s.rows[grades.GradesTable] = []source.SourceRow{{"ID": json.RawMessage(`"G1"`), "XH": json.RawMessage(`"OTHER"`), "XN": json.RawMessage(`"2025-2026"`), "XQM": json.RawMessage(`"1"`), "JD": json.RawMessage(`"malformed"`)}}
	o, h, at := onlineFixture(t, s)
	o.FixedCases = map[contract.Operation][]json.RawMessage{contract.StudentGrades: {json.RawMessage(`{"student_number":"FIXED","academic_year":"all"}`)}}
	count, _, err := o.Check(t.Context(), h, contract.StudentGrades, at)
	if err != nil || count != 2 {
		t.Fatal("fixed query did not use current candidate", count, err)
	}
	// Once this row belongs to the fixed student, actual service validation fails.
	s.rows[grades.GradesTable][0]["XH"] = json.RawMessage(`"FIXED"`)
	if _, _, err = o.Check(t.Context(), h, contract.StudentGrades, at); err == nil {
		t.Fatal("fixed query replaced with historical response")
	}
}

func TestLegacyPublishedBusinessEvidenceRemainsReadable(t *testing.T) {
	_, target, m, _, _, _ := buildFixture(t)
	for _, policy := range []string{"", "bounded-smoke-v1", "bounded-smoke-v2"} {
		legacy := candidate.Evidence{Target: target, Combination: m.Combination, CheckedAt: time.Now(), Queries: map[contract.Operation]candidate.QueryEvidence{}}
		for _, def := range contract.LegacyBusinessDefinitions() {
			names, err := metadata.Dependencies(def.Operation)
			if err != nil {
				t.Fatal(err)
			}
			deps := map[string]string{}
			for _, name := range names {
				for _, table := range m.Tables {
					if table.Table == name {
						deps[name] = table.Scope + ":" + table.SHA256
					}
				}
			}
			legacy.Queries[def.Operation] = candidate.QueryEvidence{Policy: policy, Dependencies: deps, Cases: 1, SHA256: strings.Repeat("a", 64)}
		}
		if err := legacy.Validate(target, m); err != nil {
			t.Fatal("published legacy proof rejected", policy, err)
		}
		state := &validations{previous: legacy, fail: string(contract.StaffTimetable)}
		got, err := validator(state).Validate(t.Context(), target, m)
		if err != nil || got.Policy != candidate.StorageCheckPolicy || len(got.Queries) != 0 {
			t.Fatal("upgrade did not create storage proof", err)
		}
	}
}
