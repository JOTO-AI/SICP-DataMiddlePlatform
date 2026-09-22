package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

// Canonical retains number tokens, decoded string contents, key presence and
// array order. Object key order is insignificant; duplicate nested object
// keys retain their relative order and multiplicity rather than being dropped.
func (r Record) Canonical() ([]byte, error) {
	names := make([]string, 0, len(r.Fields))
	for name := range r.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	var out bytes.Buffer
	out.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			out.WriteByte(',')
		}
		key, _ := json.Marshal(name)
		out.Write(key)
		out.WriteByte(':')
		d := json.NewDecoder(bytes.NewReader(r.Fields[name].Raw))
		d.UseNumber()
		value, err := canonicalValue(d, r.Fields[name].Raw)
		if err != nil {
			return nil, err
		}
		if _, err := d.Token(); err != io.EOF {
			return nil, errors.New("invalid source token")
		}
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func canonicalValue(d *json.Decoder, raw []byte) ([]byte, error) {
	start := d.InputOffset()
	token, err := d.Token()
	if err != nil {
		return nil, errors.New("invalid source token")
	}
	if number, ok := token.(json.Number); ok {
		return []byte(number.String()), nil
	}
	if text, ok := token.(string); ok {
		encoded := bytes.TrimLeft(raw[start:d.InputOffset()], " \t\r\n,:")
		// JSON permits escape sequences that cannot be represented as SQL TEXT.
		// Do not let encoding/json replace unpaired surrogates with U+FFFD.
		if _, err := value.Bind(value.Text, value.Field{Present: true, Raw: encoded}); err != nil {
			return append([]byte(nil), encoded...), nil
		}
		return json.Marshal(text)
	}
	if delimiter, ok := token.(json.Delim); ok {
		var out bytes.Buffer
		switch delimiter {
		case '[':
			out.WriteByte('[')
			i := 0
			for d.More() {
				value, err := canonicalValue(d, raw)
				if err != nil {
					return nil, err
				}
				if i > 0 {
					out.WriteByte(',')
				}
				out.Write(value)
				i++
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return nil, errors.New("invalid source token")
			}
			out.WriteByte(']')
		case '{':
			type pair struct {
				key        string
				encodedKey []byte
				value      []byte
			}
			var pairs []pair
			for d.More() {
				key, err := canonicalValue(d, raw)
				if err != nil {
					return nil, errors.New("invalid source token")
				}
				var name string
				if json.Unmarshal(key, &name) != nil {
					return nil, errors.New("invalid source token")
				}
				value, err := canonicalValue(d, raw)
				if err != nil {
					return nil, err
				}
				pairs = append(pairs, pair{name, key, value})
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return nil, errors.New("invalid source token")
			}
			sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
			out.WriteByte('{')
			for i, pair := range pairs {
				if i > 0 {
					out.WriteByte(',')
				}
				out.Write(pair.encodedKey)
				out.WriteByte(':')
				out.Write(pair.value)
			}
			out.WriteByte('}')
		default:
			return nil, errors.New("invalid source token")
		}
		return out.Bytes(), nil
	}
	return json.Marshal(token)
}
