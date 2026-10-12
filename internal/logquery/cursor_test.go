package logquery_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
)

func cursorFixture(t *testing.T, direction logquery.Direction) (*logquery.Signer, logquery.Filter, string) {
	t.Helper()
	s, err := logquery.NewSigner(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	f := logquery.Filter{Mode: logquery.HistoryMode, From: instant().Add(-time.Hour), To: instant(), PageSize: 50}
	if direction == logquery.Stream {
		f.Mode = logquery.LiveMode
		f.To = time.Time{}
	}
	token, err := s.Mint(logquery.Platform, "epoch-1", direction, f, "operator-001/logs_read", 100, 100, 20, instant())
	if err != nil {
		t.Fatal(err)
	}
	return s, f, token
}

func TestT27_CursorBindsSourceDirectionScopeAndSigningKey(t *testing.T) {
	s, _, token := cursorFixture(t, logquery.History)
	for _, tc := range []struct {
		name      string
		source    logquery.Source
		direction logquery.Direction
		scope     string
	}{
		{"source", logquery.Upstream, logquery.History, "operator-001/logs_read"},
		{"direction", logquery.Platform, logquery.Stream, "operator-001/logs_read"},
		{"scope", logquery.Platform, logquery.History, "operator-002/logs_read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := logquery.ErrCursor
			if tc.name == "scope" {
				want = logquery.ErrForbidden
			}
			if _, err := s.Decode(token, tc.source, tc.direction, tc.scope, instant()); err != want {
				t.Fatal("cursor boundary not enforced", err)
			}
		})
	}
	other, _ := logquery.NewSigner(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Decode(token, logquery.Platform, logquery.History, "operator-001/logs_read", instant()); err != logquery.ErrCursor {
		t.Fatal("foreign key accepted", err)
	}
	for _, bad := range []string{"", token + "x", "x" + token[1:], strings.Repeat("a", 4097), token + "\n", "not.a.token"} {
		if _, err := s.Decode(bad, logquery.Platform, logquery.History, "operator-001/logs_read", instant()); err != logquery.ErrCursor {
			t.Fatal("tampered cursor accepted", err)
		}
	}
}

func TestT27_CursorExpiryAndPagingDoNotExtendLease(t *testing.T) {
	s, f, token := cursorFixture(t, logquery.History)
	c, err := s.Decode(token, logquery.Platform, logquery.History, "operator-001/logs_read", instant().Add(time.Hour))
	if err != nil || c.ValidateContinuation(f, "epoch-1", 20) != nil {
		t.Fatal("fresh history after prior retention rejected", err)
	}
	c.Position = 80
	continued, err := s.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decode(continued, logquery.Platform, logquery.History, "operator-001/logs_read", instant().Add(24*time.Hour)); err != logquery.ErrExpired {
		t.Fatal("paging extended expiry", err)
	}
	if _, err = s.Decode(token, logquery.Platform, logquery.History, "operator-001/logs_read", instant().Add(-time.Second)); err != logquery.ErrCursor {
		t.Fatal("future token accepted", err)
	}
	if c.ValidateContinuation(f, "epoch-1", 21) != logquery.ErrExpired {
		t.Fatal("history survived retention after snapshot")
	}
	if c.ValidateContinuation(f, "epoch-1", 19) != logquery.ErrCursor {
		t.Fatal("regressing retention accepted")
	}
	f.PageSize = 51
	if c.ValidateContinuation(f, "epoch-1", 20) != logquery.ErrCursor {
		t.Fatal("page size change accepted")
	}
	f = c.Filter
	f.Kind = "request"
	if c.ValidateContinuation(f, "epoch-1", 20) != logquery.ErrCursor {
		t.Fatal("filter change accepted")
	}
}

func TestT27_StreamRetentionAndLargeSequencePrecision(t *testing.T) {
	s, f, token := cursorFixture(t, logquery.Stream)
	c, err := s.Decode(token, logquery.Platform, logquery.Stream, "operator-001/logs_read", instant())
	if err != nil {
		t.Fatal(err)
	}
	if c.ValidateContinuation(f, "epoch-1", 100) != nil || c.ValidateContinuation(f, "epoch-1", 101) != logquery.ErrExpired {
		t.Fatal("stream retention boundary incorrect")
	}
	c.HighWater = 9007199254740993
	c.Position = c.HighWater + 1
	token, err = s.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Decode(token, logquery.Platform, logquery.Stream, "operator-001/logs_read", instant())
	if err != nil || got.HighWater != c.HighWater || got.Position != c.Position {
		t.Fatal("large sequence lost precision", err)
	}
}

func TestT27_SignerRejectsInvalidConfigurationAndHidesKey(t *testing.T) {
	for _, key := range [][]byte{nil, make([]byte, 32), bytes.Repeat([]byte{1}, 31)} {
		if _, err := logquery.NewSigner(key); err != logquery.ErrCursor {
			t.Fatal("invalid key accepted")
		}
	}
	key := bytes.Repeat([]byte("a"), 32)
	s, err := logquery.NewSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'b'
	if strings.Contains(fmt.Sprintf("%v %#v", s, s), strings.Repeat("a", 16)) {
		t.Fatal("signer exposes key")
	}
	f := logquery.Filter{Mode: logquery.HistoryMode, From: instant().Add(-time.Hour), To: instant(), PageSize: 50}
	if _, err = s.Mint(logquery.Platform, "epoch-1", logquery.History, f, "", 100, 100, 0, instant()); err != logquery.ErrCursor {
		t.Fatal("empty scope accepted")
	}
	if _, err = s.Mint(logquery.Platform, "epoch-1", logquery.History, f, "admin", 100, 100, 101, instant()); err != logquery.ErrCursor {
		t.Fatal("invalid retention accepted")
	}
}
