package compute

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrValue = errors.New("invalid source field value")

func Text(row source.SourceRow, key string) (*string, error) {
	raw := bytes.TrimSpace(row[key])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return nil, ErrValue
	}
	return &v, nil
}
func Required(row source.SourceRow, key string) (string, error) {
	v, err := Text(row, key)
	if err != nil || v == nil || strings.TrimSpace(*v) == "" {
		return "", ErrValue
	}
	return *v, nil
}

// Number accepts source JSON numbers and numeric strings. Only score fields
// permit nonnumeric text, which has the explicitly agreed null presentation.
func Number(row source.SourceRow, key string, textScore bool) (*float64, error) {
	raw := bytes.TrimSpace(row[key])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	token := string(raw)
	quoted := raw[0] == '"'
	if quoted {
		if json.Unmarshal(raw, &token) != nil {
			return nil, ErrValue
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, nil
		}
	} else if !json.Valid(raw) || !(raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')) {
		return nil, ErrValue
	}
	v, err := strconv.ParseFloat(token, 64)
	if err != nil {
		if quoted && textScore && !errors.Is(err, strconv.ErrRange) {
			return nil, nil
		}
		return nil, ErrValue
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		if quoted && textScore {
			return nil, nil
		}
		return nil, ErrValue
	}
	return &v, nil
}

type Index map[string]source.SourceRow

func IndexRows(rows []source.SourceRow, key string) (Index, error) {
	i := Index{}
	for _, row := range rows {
		id, err := Required(row, key)
		if err != nil {
			return nil, err
		}
		if _, exists := i[id]; exists {
			return nil, ErrRecords
		}
		i[id] = row
	}
	return i, nil
}

// Optional returns an empty value for absent/null/blank source text.
// Present non-string values remain errors; identifiers are never trimmed.
func Optional(row source.SourceRow, key string) (string, error) {
	v, err := Text(row, key)
	if err != nil {
		return "", err
	}
	if v == nil || strings.TrimSpace(*v) == "" {
		return "", nil
	}
	return *v, nil
}
