package config

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ReadEnv reads Compose's raw KEY=value format. Values are literal: no shell
// execution, interpolation, quote removal, exports, or multiline secrets.
func ReadEnv(input io.Reader) (map[string]string, error) {
	out := map[string]string{}
	s := bufio.NewScanner(io.LimitReader(input, 1<<20+1))
	s.Buffer(make([]byte, 4096), 1<<20)
	total := 0
	for s.Scan() {
		line := strings.TrimSuffix(s.Text(), "\r")
		total += len(line) + 1
		if total > 1<<20 {
			return nil, errors.New("environment file too large")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.ContainsRune(value, 0) {
			return nil, errors.New("invalid environment entry")
		}
		for n, c := range key {
			if !(c == '_' || c >= 'A' && c <= 'Z' || n > 0 && c >= '0' && c <= '9') {
				return nil, errors.New("invalid environment key")
			}
		}
		if _, ok = out[key]; ok {
			return nil, errors.New("duplicate environment key")
		}
		out[key] = value
	}
	if s.Err() != nil {
		return nil, errors.New("environment file read failed")
	}
	return out, nil
}
