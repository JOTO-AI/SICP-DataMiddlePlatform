package config_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
)

func TestT29_12ManagedDefaultsAndExplicitKeyringValidation(t *testing.T) {
	c := config.Runtime{AuditKey: strings.Repeat("a", 32), SnapshotKey: strings.Repeat("s", 32)}
	if signer, e := c.LogQuerySigner(); e != nil || signer == nil {
		t.Fatal("default management configuration missing")
	}
	c.LogInternalToken = strings.Repeat("i", 32)
	if _, e := c.LogQuerySigner(); e == nil {
		t.Fatal("partial keyring accepted")
	}
	c.LogCursorActive = "v1"
	for _, derived := range []bool{false, true} {
		key := []byte(c.AuditKey)
		if derived {
			hash := sha256.Sum256(key)
			key = hash[:]
		}
		raw, _ := json.Marshal(map[string]string{"v1": base64.StdEncoding.EncodeToString(key)})
		c.LogCursorKeys = string(raw)
		if _, e := c.LogQuerySigner(); e == nil {
			t.Fatal("reused audit secret")
		}
	}
	raw, _ := json.Marshal(map[string]string{"v1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("z", 32)))})
	c.LogCursorKeys = string(raw)
	if signer, e := c.LogQuerySigner(); e != nil || signer == nil {
		t.Fatal("valid independent keyring", e)
	}
}

func TestManagedLogKeysSurviveRestartAndSeparatePurposes(t *testing.T) {
	one := config.Runtime{SnapshotKey: strings.Repeat("s", 32)}
	two := config.Runtime{SnapshotKey: one.SnapshotKey, AdminPassword: "changed-admin-password"}
	a, err := one.LogQuerySigner()
	if err != nil {
		t.Fatal(err)
	}
	b, err := two.LogQuerySigner()
	if err != nil || one.LogManagementToken() != two.LogManagementToken() || one.LogManagementToken() == one.SnapshotKey {
		t.Fatal("unstable internal credential", err)
	}
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	f := logquery.Filter{Mode: logquery.HistoryMode, From: now.Add(-time.Hour), To: now, PageSize: 50}
	token, err := a.Mint(logquery.Platform, "epoch-1", logquery.History, f, "admin", 10, 10, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Decode(token, logquery.Platform, logquery.History, "admin", now); err != nil {
		t.Fatal("replica cannot read cursor", err)
	}
	internalKey, _ := base64.RawURLEncoding.DecodeString(one.LogManagementToken())
	wrongPurpose, _ := logquery.NewKeyring("managed-v1", map[string][]byte{"managed-v1": internalKey})
	if _, err := wrongPurpose.Decode(token, logquery.Platform, logquery.History, "admin", now); err == nil {
		t.Fatal("internal auth credential can forge a cursor")
	}
	two.SnapshotKey = strings.Repeat("x", 32)
	b, _ = two.LogQuerySigner()
	if _, err := b.Decode(token, logquery.Platform, logquery.History, "admin", now); err == nil || one.LogManagementToken() == two.LogManagementToken() {
		t.Fatal("key rotation retained old cursor or credential")
	}
}
