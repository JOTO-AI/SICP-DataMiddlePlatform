package application

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// Probe failures are not an empty database or zero bytes. Configuration and
// observation are separate, and no endpoint, credential or file path escapes.
func (a *Application) OperationsStatus(ctx context.Context) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	type result struct {
		key   string
		value any
	}
	results := make(chan result, 3)
	go func() { results <- result{"dependencies", a.Status(ctx)} }()
	for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		go func() {
			maxBytes, maxFiles := a.config.LogBytes, a.config.LogFiles
			var journal observability.Journal
			if a.runtime.Files != nil {
				journal = a.runtime.Files.Journal
			}
			if source == storage.UpstreamLog {
				maxBytes, maxFiles = a.config.UpstreamLogBytes, a.config.UpstreamLogFiles
				journal = nil
				if a.runtime.Files != nil {
					journal = a.runtime.Files.UpstreamJournal
				}
			}
			_, readErr := a.config.Databases.LogReader(source)
			_, maintErr := a.config.Databases.LogMaintenance(source)
			v := map[string]any{"reader_configured": readErr == nil, "maintenance_configured": maintErr == nil, "buffer_state": "unknown", "buffer_bytes": nil, "buffer_files": nil, "buffer_max_bytes": maxBytes, "buffer_max_files": maxFiles, "storage": nil, "storage_state": "unavailable"}
			if journal != nil {
				usage, e := journal.Usage(ctx)
				if e == nil {
					v["buffer_state"], v["buffer_bytes"], v["buffer_files"] = "available", usage.Bytes, usage.Files
				} else if ctx.Err() == nil {
					v["buffer_state"] = "unavailable"
				}
			}
			if a.Maintenance != nil {
				if capacity, e := a.Maintenance.LogStorage(ctx, source); e == nil {
					v["storage"], v["storage_state"] = capacity, "available"
				} else if ctx.Err() != nil {
					v["storage_state"] = "unknown"
				}
			}
			results <- result{string(source), v}
		}()
	}
	signer, signerErr := a.config.LogQuerySigner()
	out := map[string]any{"observed_at": time.Now().UTC(), "dependencies": nil, "platform": nil, "upstream": nil, "probe_state": "complete", "credentials": map[string]bool{"source_configured": a.runtime.Source != nil, "smtp_configured": a.config.MailEnabled(), "log_cursor_configured": signerErr == nil && signer != nil}, "smtp_reachability": "unknown"}
	for range 3 {
		select {
		case r := <-results:
			out[r.key] = r.value
		case <-ctx.Done():
			out["probe_state"] = "incomplete"
			return out
		}
	}
	return out
}
