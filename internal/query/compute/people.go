package compute

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// Integer32 parses exact decimal arithmetic, without a float round-trip or
// allocating powers of ten for untrusted exponents. Source zero/negative
// integers are preserved; positive-only request validation is separate.
func Integer32(row source.SourceRow, key string) (*int, error) {
	raw := bytes.TrimSpace(row[key])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	token := string(raw)
	if raw[0] == '"' {
		if json.Unmarshal(raw, &token) != nil {
			return nil, ErrValue
		}
		if strings.TrimSpace(token) == "" {
			return nil, nil
		}
		if strings.TrimSpace(token) != token {
			return nil, ErrValue
		}
	}
	if !json.Valid([]byte(token)) || len(token) == 0 || !(token[0] == '-' || token[0] >= '0' && token[0] <= '9') {
		return nil, ErrValue
	}
	negative := token[0] == '-'
	if negative {
		token = token[1:]
	}
	mantissa, exponent := token, "0"
	if at := strings.IndexAny(token, "eE"); at >= 0 {
		mantissa, exponent = token[:at], token[at+1:]
	}
	fraction := 0
	if at := strings.IndexByte(mantissa, '.'); at >= 0 {
		fraction = len(mantissa) - at - 1
		mantissa = mantissa[:at] + mantissa[at+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		zero := 0
		return &zero, nil
	}
	exp, err := strconv.ParseInt(exponent, 10, 64)
	if err != nil || exp > int64(len(token))+10 || exp < -int64(len(token))-10 {
		return nil, ErrValue
	}
	scale := exp - int64(fraction)
	if scale < 0 {
		if -scale >= int64(len(digits)) {
			return nil, ErrValue
		}
		cut := len(digits) + int(scale)
		if strings.Trim(digits[cut:], "0") != "" {
			return nil, ErrValue
		}
		digits = digits[:cut]
	} else {
		if int64(len(digits))+scale > 10 {
			return nil, ErrValue
		}
		digits += strings.Repeat("0", int(scale))
	}
	if len(digits) > 10 {
		return nil, ErrValue
	}
	if negative {
		digits = "-" + digits
	}
	n, err := strconv.ParseInt(digits, 10, 32)
	if err != nil {
		return nil, ErrValue
	}
	result := int(n)
	return &result, nil
}

// Month accepts month-only values and full source admission dates. Validate
// the entire calendar date before reducing precision; never truncate bad days.
func Month(row source.SourceRow, key string) (*string, error) {
	return calendarValue(row, key, "2006-01", "200601", "2006-01", "20060102", "2006-01-02")
}
func Date(row source.SourceRow, key string) (*string, error) {
	return calendarValue(row, key, "2006-01-02", "20060102", "2006-01-02")
}

func calendarValue(row source.SourceRow, key, output string, layouts ...string) (*string, error) {
	v, err := Text(row, key)
	if err != nil || v == nil {
		return nil, err
	}
	if strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	for _, layout := range layouts {
		if len(*v) != len(layout) {
			continue
		}
		date, err := time.Parse(layout, *v)
		if err == nil && date.Year() >= 1 && date.Format(layout) == *v {
			result := date.Format(output)
			return &result, nil
		}
	}
	return nil, ErrValue
}

func OrganizationCategoryName(code *string) *string {
	if code == nil {
		return nil
	}
	var name string
	switch *code {
	case "1":
		name = "教学院系"
	case "5":
		name = "行政机构"
	default:
		return nil
	}
	return &name
}

func StudentCategoryName(code *string) *string {
	if code == nil {
		return nil
	}
	var name string
	switch *code {
	case "41":
		name = "专科生"
	case "432":
		name = "中高贯通生"
	default:
		return nil
	}
	return &name
}
