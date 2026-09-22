package ingest

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

type JSONType string

const (
	Null    JSONType = "null"
	String  JSONType = "string"
	Number  JSONType = "number"
	Boolean JSONType = "boolean"
	Object  JSONType = "object"
	Array   JSONType = "array"
)
const MaxRecordBytes = 1 << 20

type Token struct {
	Type JSONType
	Raw  json.RawMessage
}
type TypeDifference struct {
	Column           string
	Expected, Actual JSONType
}
type Record struct {
	Key         string
	Fields      map[string]Token
	Differences []TypeDifference
}

type ValidationKind string

const (
	InvalidRecord ValidationKind = "invalid_record"
	InvalidKey    ValidationKind = "invalid_primary_key"
	InvalidType   ValidationKind = "invalid_column_type"
	UnknownField  ValidationKind = "unknown_field"
	RecordLimit   ValidationKind = "record_limit"
)

type ValidationError struct {
	Kind  ValidationKind
	Field string
}

// Error intentionally omits raw values and untrusted source field names.
func (e *ValidationError) Error() string    { return "source validation: " + string(e.Kind) }
func (e *ValidationError) GoString() string { return e.Error() }

func DecodeRecord(tableName string, input []byte) (Record, error) {
	d, ok := definitions[tableName]
	if !ok {
		return Record{}, &ValidationError{Kind: InvalidRecord}
	}
	if len(input) > MaxRecordBytes {
		return Record{}, &ValidationError{Kind: RecordLimit}
	}
	if !utf8.Valid(input) {
		return Record{}, &ValidationError{Kind: InvalidRecord}
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return Record{}, &ValidationError{Kind: InvalidRecord}
	}
	fields := make(map[string]Token, len(d.Columns))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return Record{}, &ValidationError{Kind: InvalidRecord}
		}
		name, ok := key.(string)
		if !ok {
			return Record{}, &ValidationError{Kind: InvalidRecord}
		}
		if _, duplicate := fields[name]; duplicate {
			return Record{}, &ValidationError{Kind: InvalidRecord}
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return Record{}, &ValidationError{Kind: InvalidRecord}
		}
		fields[name] = Token{Type: tokenType(raw), Raw: raw}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return Record{}, &ValidationError{Kind: InvalidRecord}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return Record{}, &ValidationError{Kind: InvalidRecord}
	}
	known := make(map[string]bool, len(d.Columns))
	for _, column := range d.Columns {
		known[column.Name] = true
	}
	for name := range fields {
		if !known[name] {
			return Record{}, &ValidationError{Kind: UnknownField, Field: name}
		}
	}
	pk, present := fields[d.PrimaryKey]
	if !present || pk.Type != String {
		return Record{}, &ValidationError{Kind: InvalidKey, Field: d.PrimaryKey}
	}
	bound, err := value.Bind(value.Text, value.Field{Present: true, Raw: pk.Raw})
	if err != nil {
		return Record{}, &ValidationError{Kind: InvalidKey, Field: d.PrimaryKey}
	}
	key := bound.(string)
	if strings.TrimSpace(key) == "" {
		return Record{}, &ValidationError{Kind: InvalidKey, Field: d.PrimaryKey}
	}
	record := Record{Key: key, Fields: fields, Differences: []TypeDifference{}}
	for _, column := range d.Columns {
		token, present := fields[column.Name]
		if !present {
			continue
		}
		if _, err := value.Bind(column.Kind, value.Field{Present: true, Raw: token.Raw}); err != nil {
			return Record{}, &ValidationError{Kind: InvalidType, Field: column.Name}
		}
		if column.Kind == value.JSON && token.Type != Number {
			record.Differences = append(record.Differences, TypeDifference{Column: column.Name, Expected: Number, Actual: token.Type})
		}
	}
	return record, nil
}

func tokenType(raw []byte) JSONType {
	switch bytes.TrimSpace(raw)[0] {
	case 'n':
		return Null
	case '"':
		return String
	case 't', 'f':
		return Boolean
	case '{':
		return Object
	case '[':
		return Array
	default:
		return Number
	}
}

// SourceRow detaches tokens from the decoded record. Missing keys stay missing;
// an explicit null stays a present, non-nil raw JSON token.
func (r Record) SourceRow() source.SourceRow {
	result := make(source.SourceRow, len(r.Fields))
	for name, token := range r.Fields {
		result[name] = append(json.RawMessage(nil), token.Raw...)
	}
	return result
}
