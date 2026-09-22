package ingest_test

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

func TestT03_12ExistingColumnFirstAppearanceIsAccepted(t *testing.T) {
	// XMPY is in the confirmed 274-column catalog even when a previous page
	// omitted it. Samples are not used as a runtime schema whitelist.
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XH":"0001","XMPY":"Synthetic"}]}}`))), nil
	}))
	r := pageRequest()
	r.Table = "dwd_bzksjbsjzlb"
	result, err := client.FetchPage(context.Background(), r, retryPolicy())
	if err != nil || len(result.Attempts) != 1 || len(result.Page.Records) != 1 || result.FinalFailure != nil || string(result.Page.Records[0].Fields["XMPY"].Raw) != `"Synthetic"` {
		t.Fatalf("known column rejected: %v", err)
	}
}

func TestT03_13UnknownValuesAndNullRetryThenProduceOneFinalEvent(t *testing.T) {
	for _, mode := range []ingest.Mode{ingest.Full, ingest.Incremental} {
		for _, value := range []string{`"synthetic-value"`, `null`} {
			calls := 0
			original := ""
			client := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					original = r.URL.String()
				} else if original != r.URL.String() {
					t.Fatal("unknown-field retry changed parameters")
				}
				return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":2,"list":[{"XQH":"01","Z_EXTRA":`+value+`},{"XQH":"02","A_EXTRA":null}]}}`))), nil
			}))
			r := pageRequest()
			r.Mode = mode
			r.StartTime = "2026-08-05 07:55:00"
			r.EndTime = "2026-08-05 10:00:00"
			result, err := client.FetchPage(context.Background(), r, retryPolicy())
			e := result.FinalFailure
			if err == nil || calls != 3 || len(result.Attempts) != 3 || result.Page.Records != nil || e == nil {
				t.Fatal("unknown field accepted or final event missing")
			}
			if e.Source != "dwd_xqjbsjzlb" || e.Path != "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb" || e.Mode != mode || e.Page != 1 || e.PageSize != 2 || e.Attempts != 3 || e.Reason != ingest.RecordFailure || !reflect.DeepEqual(e.UnknownFields, []string{"A_EXTRA", "Z_EXTRA"}) {
				t.Fatal("incorrect final event")
			}
		}
	}
}

func TestT03_14RecoveredUnknownFieldProducesNoFinalEvent(t *testing.T) {
	calls := 0
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		extra := ``
		if calls == 1 {
			extra = `,"EXTRA":null`
		}
		return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01"`+extra+`}]}}`))), nil
	}))
	r, err := client.FetchPage(context.Background(), pageRequest(), retryPolicy())
	if err != nil || calls != 2 || len(r.Page.Records) != 1 || len(r.Attempts) != 2 || r.FinalFailure != nil {
		t.Fatal("recovered error notified or duplicated page")
	}
}

func TestT03_13TimeBudgetExhaustionStillProducesFinalUnknownEvent(t *testing.T) {
	clock := fakeBudgetClock()
	calls := 0
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01","EXTRA":null}]}}`))), nil
	}))
	cfg := configBudget()
	cfg.PageTimeout = time.Second
	c := coordinator(t, client, cfg, clock, &savedCooldown{}, freeAdmission())
	r, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 10))
	if err == nil || calls != 1 || r.FinalFailure == nil || r.FinalFailure.Attempts != 1 || r.FinalFailure.Reason != ingest.BudgetFailure || len(r.FinalFailure.UnknownFields) != 1 {
		t.Fatal("budget exhaustion lost unknown-field event")
	}
}

func TestT03_13LaterNetworkFailureRetainsObservedUnknownFields(t *testing.T) {
	calls := 0
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			return nil, temporaryError{}
		}
		return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01","EXTRA":null}]}}`))), nil
	}))
	r, err := client.FetchPage(context.Background(), pageRequest(), retryPolicy())
	if err == nil || calls != 3 || r.FinalFailure == nil || r.FinalFailure.Reason != ingest.TransportFailure || !reflect.DeepEqual(r.FinalFailure.UnknownFields, []string{"EXTRA"}) {
		t.Fatal("later failure hid unknown-field evidence")
	}
}

func TestT03_13LateUnknownResponseRetainsEvidenceOnBudgetFailure(t *testing.T) {
	clock := fakeBudgetClock()
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		clock.now = clock.now.Add(40 * time.Second)
		return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01","EXTRA":null}]}}`))), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	r, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 10))
	if err == nil || r.FinalFailure == nil || r.FinalFailure.Reason != ingest.BudgetFailure || !reflect.DeepEqual(r.FinalFailure.UnknownFields, []string{"EXTRA"}) {
		t.Fatal("late response lost schema evidence")
	}
}
