package logquery_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
)

func TestT27_01DTOEmptyNullAndDecimalFacts(t *testing.T) {
	body, err := json.Marshal(logquery.Page{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"items":[]`, `"next_cursor":null`, `"stream_cursor":null`, `"high_water":"0"`, `"retained_through":"0"`, `"expires_at":null`} {
		if !bytes.Contains(body, []byte(expected)) {
			t.Fatalf("missing %s in %s", expected, body)
		}
	}
	zero := int64(0)
	body, err = json.Marshal(logquery.EventItem{Sequence: 9007199254740993, DurationNS: &zero})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"sequence":"9007199254740993"`, `"http_status":null`, `"duration_ns":"0"`, `"audit":null`, `"test":null`, `"occurred_at":null`, `"received_at":null`} {
		if !bytes.Contains(body, []byte(expected)) {
			t.Fatalf("missing %s in %s", expected, body)
		}
	}
	correlation := strings.Repeat("a", 64)
	if (logquery.TestIdentity{Purpose: logquery.TestPurpose, TestRunID: correlation}).Validate() != nil || (logquery.TestIdentity{Purpose: logquery.SyncPurpose}).Validate() != nil {
		t.Fatal("valid purpose rejected")
	}
	for _, identity := range []logquery.TestIdentity{{}, {Purpose: logquery.TestPurpose}, {Purpose: logquery.SyncPurpose, TestRunID: correlation}, {Purpose: logquery.TestPurpose, TestRunID: "raw-id"}} {
		if identity.Validate() == nil {
			t.Fatal("invalid purpose accepted")
		}
	}
	if (logquery.AuditIdentity{ActorID: correlation, Action: "source_test"}).Validate() != nil {
		t.Fatal("audit rejected")
	}
	for _, audit := range []logquery.AuditIdentity{{}, {ActorID: "raw-actor", Action: "cleanup"}, {ActorID: correlation, Action: "https://untrusted"}, {ActorID: correlation, Action: "cleanup", ObjectID: "raw-object"}} {
		if audit.Validate() == nil {
			t.Fatal("invalid audit accepted")
		}
	}
}

func TestT27_02ExplicitIdentityFormatAndSingleDigest(t *testing.T) {
	// Independent HMAC-SHA256 fixture: 32 bytes of 0x03 and ASCII task_001-A.
	const digest = "ea769cde1848a0d1aeadaf92ffe60d5259c6a27fdb6f0eeea5fcca9949ef726c"
	f, _, err := parseFilter(url.Values{"task_id": {"task_001-A"}, "test_run_id": {"task_001-A"}, "kind": {"audit"}}, instant(), nil)
	if err != nil || f.TaskID != digest || f.TestRunID != digest {
		t.Fatal("raw identity not normalized", err, f)
	}
	again, _, err := parseFilter(url.Values{"task_id": {digest}, "id_format": {"correlation"}}, instant().Add(time.Hour), &f)
	if err != nil || again != f {
		t.Fatal("correlation was hashed again", err)
	}
	raw, _, err := parseFilter(url.Values{"task_id": {digest}}, instant(), nil)
	if err != nil || raw.TaskID == digest {
		t.Fatal("raw format inferred from length", err)
	}
	for _, values := range []url.Values{
		{"id_format": {"unknown"}}, {"id_format": {"raw", "correlation"}},
		{"id_format": {"correlation"}, "task_id": {"task_001-A"}},
		{"id_format": {"correlation"}, "request_id": {strings.Repeat("A", 64)}},
		{"test_run_id": {""}}, {"test_run_id": {"x y"}},
	} {
		if _, _, err := parseFilter(values, instant(), nil); err == nil {
			t.Fatal("invalid identity accepted", values)
		}
	}
	if _, _, err := logquery.ParseFilter(url.Values{"task_id": {"raw-id"}}, instant(), nil, nil); err == nil {
		t.Fatal("missing correlator accepted")
	}
}

func TestT27_03TimePrecisionAndLiveBoundaries(t *testing.T) {
	for _, values := range []url.Values{
		{"mode": {"live"}, "to": {"2026-10-12T04:00:00Z"}},
		{"mode": {"live"}, "from": {"2026-10-12T04:00:00.000001Z"}},
		{"mode": {"live"}, "from": {"2026-10-05T03:59:59.999999Z"}},
		{"from": {"2026-10-11T04:00:00.0000000Z"}},
		{"from": {"2026-10-11T04:00:00,000001Z"}},
		{"from": {"2026-10-11T4:00:00Z"}},
		{"from": {"2026-10-11T04:00:00+24:00"}},
		{"from": {"2026-10-11T04:00:00+00:60"}},
		{"mode": {"unknown"}}, {"from": {"1999-12-31T23:59:59Z"}},
	} {
		if _, _, err := parseFilter(values, instant(), nil); err == nil {
			t.Fatal("invalid time accepted", values)
		}
	}
	for _, from := range []string{"2026-10-05T04:00:00Z", "2026-10-12T04:00:00Z"} {
		if _, _, err := parseFilter(url.Values{"mode": {"live"}, "from": {from}}, instant(), nil); err != nil {
			t.Fatal("live boundary rejected", err)
		}
	}
	f, _, err := parseFilter(url.Values{"from": {"2026-10-11T12:00:00.123456+08:00"}}, instant(), nil)
	if err != nil || f.From.Nanosecond() != 123456000 || f.From.Location() != time.UTC {
		t.Fatal("microseconds lost", err)
	}
}

func TestT27_04LiveWindowSurvivesContinuationWithoutMoving(t *testing.T) {
	f, _, err := parseFilter(url.Values{"mode": {"live"}}, instant(), nil)
	if err != nil || !f.To.IsZero() || !f.From.Equal(instant().Add(-24*time.Hour)) {
		t.Fatal("invalid live default", err)
	}
	body, err := json.Marshal(f)
	if err != nil || !bytes.Contains(body, []byte(`"to":null`)) {
		t.Fatal("missing live null bound", err, string(body))
	}
	again, _, err := parseFilter(nil, instant().Add(30*24*time.Hour), &f)
	if err != nil || again != f {
		t.Fatal("continuation recomputed window", err)
	}
	for _, values := range []url.Values{{"mode": {"history"}}, {"page_size": {"51"}}, {"kind": {"request"}}, {"from": {"2026-10-11T05:00:00Z"}}} {
		if _, _, err := parseFilter(values, instant(), &f); err == nil {
			t.Fatal("continuation changed filter")
		}
	}
	s, err := logquery.NewSigner(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Mint(logquery.Platform, "epoch-1", logquery.History, f, "scope", 100, 100, 20, instant())
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Decode(token, logquery.Platform, logquery.History, "scope", instant())
	if err != nil || c.Filter != f {
		t.Fatal("live filter failed signed roundtrip", err)
	}
	c.Position = 30
	stream, err := c.StreamStart()
	if err != nil || stream.Position != 100 || stream.HighWater != 100 || stream.ExpiresAt != c.ExpiresAt || stream.Filter != f || stream.Epoch != c.Epoch {
		t.Fatal("live boundary moved with history paging", err)
	}
	history, _, err := parseFilter(nil, instant(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mint(logquery.Platform, "epoch-1", logquery.Stream, history, "scope", 0, 0, 0, instant()); err == nil {
		t.Fatal("history incorrectly permits live subscription")
	}
}

func TestT27_05SigningKeyRotationAndRevocation(t *testing.T) {
	_, f, token := cursorFixture(t, logquery.History)
	keys := map[string][]byte{"default": bytes.Repeat([]byte{7}, 32), "next": bytes.Repeat([]byte{8}, 32)}
	rotated, err := logquery.NewKeyring("next", keys)
	if err != nil {
		t.Fatal(err)
	}
	keys["default"][0] = 0 // Constructor must retain private copies.
	c, err := rotated.Decode(token, logquery.Platform, logquery.History, "operator-001/logs_read", instant())
	if err != nil {
		t.Fatal("retained key failed", err)
	}
	c.Position = 80
	continued, err := rotated.Encode(c)
	if err != nil || !strings.HasPrefix(continued, "next.") {
		t.Fatal("active key not used", err)
	}
	if c.ValidateContinuation(f, "epoch-1", 20) != nil {
		t.Fatal("rotation changed filter")
	}
	revoked, err := logquery.NewKeyring("next", map[string][]byte{"next": bytes.Repeat([]byte{8}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revoked.Decode(token, logquery.Platform, logquery.History, "operator-001/logs_read", instant()); err != logquery.ErrCursor {
		t.Fatal("revoked key accepted", err)
	}
	if _, err := revoked.Decode(continued, logquery.Platform, logquery.History, "operator-001/logs_read", instant().Add(24*time.Hour)); err != logquery.ErrExpired {
		t.Fatal("rotation extended lease", err)
	}
	for _, keys := range []map[string][]byte{nil, {"bad.id": bytes.Repeat([]byte{7}, 32)}, {"next": make([]byte, 32)}} {
		if _, err := logquery.NewKeyring("next", keys); err == nil {
			t.Fatal("invalid keyring accepted")
		}
	}
	var zero logquery.Signer
	if _, err := zero.Encode(c); err == nil {
		t.Fatal("zero signer accepted")
	}
}

func TestT27_07RestoredEpochInvalidatesBothDirections(t *testing.T) {
	for _, direction := range []logquery.Direction{logquery.History, logquery.Stream} {
		s, f, token := cursorFixture(t, direction)
		c, err := s.Decode(token, logquery.Platform, direction, "operator-001/logs_read", instant())
		if err != nil {
			t.Fatal(err)
		}
		if c.ValidateContinuation(f, "epoch-restored", 0) != logquery.ErrExpired {
			t.Fatal("restored database continued old cursor")
		}
		if c.ValidateContinuation(f, "", 20) != logquery.ErrCursor {
			t.Fatal("missing epoch accepted")
		}
	}
}

func TestT27_09BoundedRequestAndStableErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"boundary", `{}` + strings.Repeat(" ", logquery.MaxRequestBytes-2), nil},
		{"oversize", `{}` + strings.Repeat(" ", logquery.MaxRequestBytes-1), logquery.ErrTooLarge},
		{"malformed-oversize", strings.Repeat("x", logquery.MaxRequestBytes+1), logquery.ErrTooLarge},
		{"unknown", `{"password":"synthetic-secret"}`, logquery.ErrArgument},
		{"trailing", `{} {}`, logquery.ErrArgument},
		{"null", `null`, logquery.ErrArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var target struct{}
			if err := logquery.DecodeRequest(strings.NewReader(tc.body), &target); err != tc.want {
				t.Fatal("wrong parse result", err)
			}
		})
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{logquery.ErrArgument, 400, "invalid_argument"}, {logquery.ErrCursor, 400, "invalid_argument"},
		{logquery.ErrForbidden, 403, "forbidden"}, {logquery.ErrNotFound, 404, "not_found"},
		{logquery.ErrExpired, 409, "cursor_expired"}, {logquery.ErrTooLarge, 413, "request_too_large"},
		{logquery.ErrLimit, 429, "resource_exhausted"}, {logquery.ErrProxy, 502, "proxy_unavailable"},
		{logquery.ErrUnavailable, 503, "unavailable"}, {logquery.ErrTimeout, 504, "deadline_exceeded"},
		{errors.New("password=synthetic-secret"), 503, "unavailable"},
	} {
		status, code := logquery.HTTPError(fmt.Errorf("wrapped: %w", tc.err))
		if status != tc.status || code != tc.code {
			t.Fatal("unstable error mapping", status, code)
		}
	}
}
