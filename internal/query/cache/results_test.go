package cache_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
)

type command struct {
	name string
	args []any
}
type redisMemory struct {
	values         map[string][]byte
	calls          []command
	getErr, setErr error
	ack            any
}

func (r *redisMemory) Do(_ context.Context, name string, args ...any) (any, error) {
	r.calls = append(r.calls, command{name, args})
	switch name {
	case "GET":
		if r.getErr != nil {
			return nil, r.getErr
		}
		v, ok := r.values[args[0].(string)]
		if !ok {
			return nil, nil
		}
		return bytes.Clone(v), nil
	case "SET":
		if r.setErr != nil {
			return nil, r.setErr
		}
		if r.ack != "OK" {
			return r.ack, nil
		}
		if r.values == nil {
			r.values = map[string][]byte{}
		}
		r.values[args[0].(string)] = bytes.Clone(args[1].([]byte))
		return "OK", nil
	default:
		panic("unexpected Redis command")
	}
}
func results(r *redisMemory, observed *int) cache.Results {
	return cache.Results{Store: cache.Redis{Client: r}, Observe: func(error) { *observed++ }}
}

func TestT11_04ColdThenHotCacheUsesSETWithoutTTL(t *testing.T) {
	r := &redisMemory{ack: "OK"}
	observed, loads := 0, 0
	store := results(r, &observed)
	key := "query:C1:students.grades.query:page=1&page_size=100&student_number=001"
	payload := []byte(`{"total":1,"items":[{"grade_id":"G1"}]}`)
	load := func(context.Context) ([]byte, error) { loads++; return payload, nil }
	first, err := store.GetOrLoad(t.Context(), key, load)
	if err != nil || first.Hit || !bytes.Equal(first.Data, payload) {
		t.Fatal(first, err)
	}
	second, err := store.GetOrLoad(t.Context(), key, load)
	if err != nil || !second.Hit || !bytes.Equal(second.Data, payload) || loads != 1 || observed != 0 {
		t.Fatal(second, err, loads, observed)
	}
	want := []command{{"GET", []any{key}}, {"SET", []any{key, payload}}, {"GET", []any{key}}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatal(r.calls)
	}
	second.Data[0] = '!'
	if r.values[key][0] != '{' {
		t.Fatal("mutable result corrupted stored data")
	}
}

func TestT11_05EmptyResultsCacheButSQLFailuresDoNot(t *testing.T) {
	for _, payload := range []string{`[]`, `{"total":0,"items":[]}`} {
		r := &redisMemory{ack: "OK"}
		observed := 0
		store := results(r, &observed)
		loads := 0
		load := func(context.Context) ([]byte, error) { loads++; return []byte(payload), nil }
		if _, err := store.GetOrLoad(t.Context(), "key", load); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetOrLoad(t.Context(), "key", load)
		if err != nil || !got.Hit || loads != 1 {
			t.Fatal(got, err)
		}
	}
	r := &redisMemory{ack: "OK"}
	observed := 0
	store := results(r, &observed)
	sqlErr := errors.New("query failed")
	got, err := store.GetOrLoad(t.Context(), "key", func(context.Context) ([]byte, error) { return []byte(`[]`), sqlErr })
	if !errors.Is(err, sqlErr) || got.Data != nil || len(r.calls) != 1 || len(r.values) != 0 {
		t.Fatal(got, err, r.calls)
	}
}

func TestT11_06FailedSETKeepsLegalSQLResultAndIsObserved(t *testing.T) {
	for _, r := range []*redisMemory{{setErr: errors.New("write timeout")}, {ack: "not-OK"}} {
		observed := 0
		store := results(r, &observed)
		got, err := store.GetOrLoad(t.Context(), "key", func(context.Context) ([]byte, error) { return []byte(`[]`), nil })
		if err != nil || string(got.Data) != "[]" || got.Hit || observed != 1 || len(r.calls) != 2 || len(r.values) != 0 {
			t.Fatal(got, err, observed, r.calls)
		}
	}
	r := &redisMemory{getErr: errors.New("read outage")}
	observed := 0
	store := results(r, &observed)
	if _, err := store.GetOrLoad(t.Context(), "key", func(context.Context) ([]byte, error) { t.Fatal("outage silently degraded"); return nil, nil }); err == nil {
		t.Fatal("cache failure hidden")
	}
}

func TestT11_05MalformedAndCanceledLoadsNeverPopulateCache(t *testing.T) {
	for _, payload := range []string{"", "null", "1", "{broken", strings.Repeat(" ", cache.MaxPayloadBytes) + "[]"} {
		r := &redisMemory{ack: "OK"}
		observed := 0
		store := results(r, &observed)
		if _, err := store.GetOrLoad(t.Context(), "key", func(context.Context) ([]byte, error) { return []byte(payload), nil }); !errors.Is(err, cache.ErrPayload) || len(r.calls) != 1 {
			t.Fatal("malformed populated", err, r.calls)
		}
	}
	r := &redisMemory{ack: "OK"}
	observed := 0
	store := results(r, &observed)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	got, err := store.GetOrLoad(ctx, "key", func(context.Context) ([]byte, error) { cancel(); return []byte(`[]`), nil })
	if !errors.Is(err, context.Canceled) || got.Data != nil || len(r.calls) != 1 {
		t.Fatal(got, err, r.calls)
	}
	r.values = map[string][]byte{"key": []byte("invalid")}
	if _, err = store.GetOrLoad(t.Context(), "key", func(context.Context) ([]byte, error) {
		t.Fatal("corrupt cache silently treated as miss")
		return nil, nil
	}); !errors.Is(err, cache.ErrPayload) {
		t.Fatal(err)
	}
}
