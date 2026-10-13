package observability_test

import (
	"crypto/sha256"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// The offline relocation script uses these exact encodings when it replaces a
// journal's control-only topology binding. Keep this vector shared with Python.
func TestControlRelocationMarkerContract(t *testing.T) {
	v := map[string]string{
		"LOG_STORAGE_MODE":               "dual",
		"DATABASE_USER":                  "admin",
		"DATABASE_PASSWORD":              "synthetic-password",
		"DATABASE_LOG_USER":              "logger",
		"DATABASE_LOG_PASSWORD":          "synthetic-password",
		"DATABASE_UPSTREAM_LOG_USER":     "upstream",
		"DATABASE_UPSTREAM_LOG_PASSWORD": "synthetic-password",
		"DATABASE_M0_URL":                "postgres://db-m:5432/slot0?sslmode=disable",
		"DATABASE_M1_URL":                "postgres://db-m:5432/slot1?sslmode=disable",
		"DATABASE_S0_URL":                "postgres://db-s:5432/slot0?sslmode=disable",
		"DATABASE_S1_URL":                "postgres://db-s:5432/slot1?sslmode=disable",
		"DATABASE_LOG_URL":               "postgres://db-log:5432/logs?sslmode=disable",
		"DATABASE_UPSTREAM_LOG_URL":      "postgres://db-log:5432/upstream_logs?sslmode=disable",
	}
	key := sha256.Sum256([]byte("synthetic-audit-key"))
	s, err := observability.NewSanitizer(16384, 2048, key[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, topology, marker string }{
		{"db-m", "0f728a865530173fe96426e7484775d3dd62cba731219131d04651e21a1ed0a9", "230f2c8f2aaff35a51722d0ac28f1c7f358b0345b38f66d720344e7a9b5373da"},
		{"db-log", "b928cb867a3c79f162def6cd051708e64ede87b2cb9e2961522c058c342570a8", "199e68c92e2e008549e0ef0980bc2805ba7f483e82472af1a986ad767d5d8c5d"},
	} {
		v["DATABASE_CONTROL_URL"] = "postgres://" + tc.host + ":5432/control?sslmode=disable"
		r, err := storage.LoadRegistry(func(k string) (string, bool) { value, ok := v[k]; return value, ok })
		if err != nil || r.LogTopology() != tc.topology || s.Fingerprint("dual-log-v1", r.LogTopology()) != tc.marker {
			t.Fatal("offline migration marker contract changed", err)
		}
	}
}
