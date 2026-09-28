package application

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestWorkerApplicationOptionalMail(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			v := testkit.RuntimeValues("/data")
			if !enabled {
				for _, key := range []string{"MAIL_ADDRESS", "MAIL_TLS", "MAIL_FROM", "MAIL_TO", "MAIL_USERNAME", "MAIL_PASSWORD"} {
					delete(v, key)
				}
			}
			c, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { value, ok := v[k]; return value, ok })
			if err != nil {
				t.Fatal(err)
			}
			journal, err := observability.OpenDirectory(t.TempDir(), rand.Reader, 16384)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			key := sha256.Sum256([]byte(c.AuditKey))
			sanitizer, err := observability.NewSanitizer(16384, 2048, key[:], nil)
			if err != nil {
				t.Fatal(err)
			}
			quota, err := observability.NewQuota(journal, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 100, WarningPercent: 80, Policy: observability.RejectNew})
			if err != nil {
				t.Fatal(err)
			}
			buffer, err := observability.NewBuffer(quota, sanitizer, 10, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			r := &assembly.Runtime{Sanitizer: sanitizer, Logs: buffer}
			if enabled {
				r.Mail, err = adapters.NewMail(c)
				if err != nil {
					t.Fatal(err)
				}
			}
			a, err := New(c, r)
			if err != nil {
				t.Fatal(err)
			}
			if (a.Sender != nil) != enabled || a.Engine == nil || a.Management == nil || a.Logs == nil {
				t.Fatal("mail setting must only control the notification sender")
			}
		})
	}
}
