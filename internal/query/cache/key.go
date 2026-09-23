package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

var ErrKey = errors.New("invalid query cache identity")

// Key accepts the effective typed request from the strict transport parser.
// Route identity, not an arbitrary path suffix, determines the operation.
// No credentials, physical positions, generations or request IDs are included.
func Key(combination string, op contract.Operation, request any) (string, error) {
	if !utf8.ValidString(combination) || len(combination) > 256 || strings.TrimSpace(combination) == "" {
		return "", ErrKey
	}
	for _, ch := range combination {
		if unicode.IsControl(ch) {
			return "", ErrKey
		}
	}
	valid := false
	switch op {
	case contract.StudentGrades:
		_, valid = request.(contract.StudentGradesRequest)
	case contract.BatchGrades:
		_, valid = request.(contract.BatchGradesRequest)
	case contract.StudentTimetable:
		_, valid = request.(contract.StudentTimetableRequest)
	case contract.StaffTimetable:
		_, valid = request.(contract.StaffTimetableRequest)
	}
	if !valid {
		return "", ErrKey
	}
	data, err := json.Marshal(request)
	if err != nil {
		return "", ErrKey
	}
	values := map[string]json.RawMessage{}
	if json.Unmarshal(data, &values) != nil {
		return "", ErrKey
	}
	if op == contract.StudentGrades || op == contract.BatchGrades {
		for key, value := range map[string]string{"page": "1", "page_size": "100"} {
			if bytes.Equal(values[key], []byte("null")) {
				values[key] = json.RawMessage(value)
			}
		}
	}
	keys := make([]string, 0, len(values))
	for key, raw := range values {
		if !bytes.Equal(raw, []byte("null")) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		raw := values[key]
		value := ""
		switch raw[0] {
		case '"':
			if json.Unmarshal(raw, &value) != nil {
				return "", ErrKey
			}
		case '[':
			var items []string
			if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
				return "", ErrKey
			}
			sort.Strings(items)
			unique := items[:0]
			for _, item := range items {
				if len(unique) == 0 || unique[len(unique)-1] != item {
					unique = append(unique, item)
				}
			}
			encoded, err := json.Marshal(unique)
			if err != nil {
				return "", ErrKey
			}
			value = string(encoded)
		default:
			// The DTO has only integers and finite float score bounds here.
			if key == "page" || key == "page_size" {
				value = string(raw)
				break
			}
			n, err := strconv.ParseFloat(string(raw), 64)
			if err != nil {
				return "", ErrKey
			}
			if n == 0 {
				value = "0"
			} else {
				value = strconv.FormatFloat(n, 'g', -1, 64)
			}
		}
		parts = append(parts, escape(key)+"="+escape(value))
	}
	params := strings.Join(parts, "&")
	if params == "" {
		params = "-"
	}
	return "query:" + escape(combination) + ":" + escape(string(op)) + ":" + params, nil
}

// RFC 3986 encoding over UTF-8 bytes; spaces are %20, never '+'.
func escape(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~", rune(b)) {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[b>>4])
			out.WriteByte(hex[b&15])
		}
	}
	return out.String()
}
