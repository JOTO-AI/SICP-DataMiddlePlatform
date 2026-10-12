package assembly

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func dualFixture(t *testing.T) config.Runtime {
	t.Helper()
	base, v := fixture(t)
	v["LOG_STORAGE_MODE"] = "dual"
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:synthetic@upstream:5432/logs?sslmode=disable"
	v["UPSTREAM_LOG_DIRECTORY"] = filepath.Join(filepath.Dir(base.LogDirectory), "upstream-logs")
	v["UPSTREAM_LOG_MAX_BYTES"] = "1048576"
	v["UPSTREAM_LOG_MAX_FILES"] = "1000"
	c, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { value, ok := v[k]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(c.UpstreamLogDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestT28_10AssemblyConnectsIndependentDurableSources(t *testing.T) {
	c := dualFixture(t)
	var trace []string
	r, _, err := Open(t.Context(), c, trackedFactories(&trace, ""))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	defer r.Close(ctx)
	if _, ok := r.LogDatabase.(storage.VerifiedLogs); !ok || r.Files.Journal == r.Files.UpstreamJournal || r.Logs == r.UpstreamLogs {
		t.Fatal("dual resources not isolated")
	}
	guard, err := observability.NewGuard(r.Sanitizer, r.LogSink)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []events.Kind{events.Request, events.Call} {
		e := events.Event{ID: string(kind), EntityID: "entity", Kind: kind, Phase: events.Started, OccurredAt: time.Now()}
		if err = guard.Record(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range []*observability.Buffer{r.Logs, r.UpstreamLogs} {
		pending, err := b.Pending(t.Context())
		if err != nil || len(pending) != 1 {
			t.Fatal("wrong durable destination")
		}
	}
}
func TestT28_11DualInitializationFailureReleasesAndPreservesFiles(t *testing.T) {
	c := dualFixture(t)
	marker := filepath.Join(c.LogDirectory, "preserved.event")
	if os.WriteFile(marker, []byte("existing"), 0600) != nil {
		t.Fatal("seed")
	}
	for _, failure := range []string{"files", "source", "mail"} {
		var trace []string
		r, results, err := Open(t.Context(), c, trackedFactories(&trace, failure))
		if r != nil || err == nil || len(results) < 3 || strings.Contains(err.Error(), "synthetic-secret") || trace[len(trace)-1] != "close:postgres" {
			t.Fatal("partial dual initialization leaked", trace)
		}
		for _, result := range results {
			if result.State != "closed" {
				t.Fatal("resource unclosed")
			}
		}
		if body, err := os.ReadFile(marker); err != nil || string(body) != "existing" {
			t.Fatal("existing file changed")
		}
	}
}
