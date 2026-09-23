package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const (
	MaxRequestBytes = 64 << 10
	MaxStringBytes  = 512
	MaxArrayItems   = 1000
	MaxPage         = 1000000
	MaxPageSize     = 1000
)

// Parse returns one of the four request values. It preserves identifier bytes;
// null optional filters are absent, while empty strings/arrays are invalid.
func Parse(op contract.Operation, body io.Reader) (any, error) {
	bad := func() (any, error) { return nil, &Failure{Category: contract.InvalidArgument} }
	if body == nil {
		return bad()
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxRequestBytes+1))
	if err != nil || len(data) > MaxRequestBytes || !utf8.Valid(data) {
		return bad()
	}
	fields, required := requestFields(op)
	if fields == nil {
		return bad()
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	start, err := dec.Token()
	if err != nil || start != json.Delim('{') {
		return bad()
	}
	values := map[string]json.RawMessage{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return bad()
		}
		key, ok := token.(string)
		if !ok || fields[key] == 0 {
			return bad()
		}
		if _, exists := values[key]; exists {
			return bad()
		}
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return bad()
		}
		values[key] = raw
		if bytes.Equal(raw, []byte("null")) {
			if required[key] {
				return bad()
			}
			continue
		}
		switch fields[key] {
		case 's':
			var value string
			if json.Unmarshal(raw, &value) != nil || !validString(value) {
				return bad()
			}
			if key == "semester" && value != "1" && value != "2" {
				return bad()
			}
			if key == "academic_year" && !validYear(value, op == contract.StudentGrades || op == contract.BatchGrades) {
				return bad()
			}
		case 'a':
			var value []*string
			if json.Unmarshal(raw, &value) != nil || len(value) == 0 || len(value) > MaxArrayItems {
				return bad()
			}
			for _, v := range value {
				if v == nil || !validString(*v) {
					return bad()
				}
			}
		case 'i':
			var value int
			if json.Unmarshal(raw, &value) != nil || value < 1 || (key == "page" && value > MaxPage) || (key == "page_size" && value > MaxPageSize) {
				return bad()
			}
		case 'n':
			var value float64
			if json.Unmarshal(raw, &value) != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return bad()
			}
		}
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim('}') {
		return bad()
	}
	if _, err = dec.Token(); err != io.EOF {
		return bad()
	}
	for key := range required {
		if _, ok := values[key]; !ok {
			return bad()
		}
	}
	switch op {
	case contract.StudentGrades:
		var v contract.StudentGradesRequest
		if json.Unmarshal(data, &v) != nil || !normalizeGrades(&v.GradeFilters) {
			return bad()
		}
		return v, nil
	case contract.BatchGrades:
		var v contract.BatchGradesRequest
		if json.Unmarshal(data, &v) != nil || !normalizeGrades(&v.GradeFilters) {
			return bad()
		}
		return v, nil
	case contract.StudentTimetable:
		var v contract.StudentTimetableRequest
		if json.Unmarshal(data, &v) != nil {
			return bad()
		}
		return v, nil
	case contract.StaffTimetable:
		var v contract.StaffTimetableRequest
		if json.Unmarshal(data, &v) != nil {
			return bad()
		}
		return v, nil
	}
	return bad()
}

func requestFields(op contract.Operation) (map[string]byte, map[string]bool) {
	fields := map[string]byte{}
	required := map[string]bool{}
	add := func(kind byte, names ...string) {
		for _, name := range names {
			fields[name] = kind
		}
	}
	switch op {
	case contract.StudentGrades, contract.BatchGrades:
		add('s', "academic_year", "semester", "course_code", "course_name", "course_department_code", "course_department_name")
		add('a', "course_attribute_codes", "course_nature_codes", "course_type_codes")
		add('i', "page", "page_size")
		add('n', "score_min", "score_max")
		if op == contract.StudentGrades {
			add('s', "student_number")
			required["student_number"] = true
		} else {
			add('s', "student_department_code", "student_department_name", "class_code", "class_name")
			add('a', "student_numbers")
			required["academic_year"] = true
		}
	case contract.StudentTimetable, contract.StaffTimetable:
		add('s', "academic_year", "semester", "course_query")
		required["academic_year"], required["semester"] = true, true
		if op == contract.StudentTimetable {
			add('s', "student_number")
			required["student_number"] = true
		} else {
			add('s', "staff_number", "teaching_class_number")
			required["staff_number"] = true
		}
	default:
		return nil, nil
	}
	return fields, required
}

func validString(v string) bool {
	if len(v) > MaxStringBytes || strings.TrimSpace(v) == "" {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 || r == utf8.RuneError {
			return false
		}
	}
	return true
}

func validYear(v string, allowAll bool) bool {
	if allowAll && v == "all" {
		return true
	}
	if len(v) != 9 || v[4] != '-' {
		return false
	}
	for i, c := range v {
		if i != 4 && (c < '0' || c > '9') {
			return false
		}
	}
	a, _ := strconv.Atoi(v[:4])
	b, _ := strconv.Atoi(v[5:])
	return a >= 1000 && b == a+1
}

func normalizeGrades(f *contract.GradeFilters) bool {
	if f.Page == nil {
		v := 1
		f.Page = &v
	}
	if f.PageSize == nil {
		v := 100
		f.PageSize = &v
	}
	return f.ScoreMin == nil || f.ScoreMax == nil || *f.ScoreMin <= *f.ScoreMax
}
