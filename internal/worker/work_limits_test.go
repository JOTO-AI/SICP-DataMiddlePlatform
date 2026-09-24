package worker

import (
	"bytes"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
)

func TestT15_09RuntimeFullBatchFitsPersistedTargetChunk(t *testing.T) {
	target, options := targetPreparation(config.Runtime{SourceScope: "synthetic", SpoolBytes: 64 << 20}, nil, "Csynthetic")
	// Exercise the boundary where every permitted row contributes its JSONL
	// delimiter in addition to a completely filled canonical-payload budget.
	var chunk bytes.Buffer
	payload := bytes.Repeat([]byte{'x'}, int(options.Limits.BatchBytes))
	chunk.Write(payload)
	for range options.Limits.BatchRows {
		chunk.WriteByte('\n')
	}
	if int64(chunk.Len()) > target.MaxChunkBytes {
		t.Fatalf("full merge batch requires %d bytes; target accepts %d", chunk.Len(), target.MaxChunkBytes)
	}
	if options.Limits.BatchBytes <= 0 || options.Limits.BatchRows <= 0 {
		t.Fatal("empty runtime merge budget")
	}
}
