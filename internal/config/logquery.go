package config

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
)

// Default deployment derives stable, purpose-separated keys from the existing
// shared snapshot secret. No additional operator-managed secret is required.
// Explicit keyrings remain supported for direct-process rolling upgrades.
func (c Runtime) LogQuerySigner() (*logquery.Signer, error) {
	if c.LogCursorKeys == "" && c.LogCursorActive == "" && c.LogInternalToken == "" {
		if len(c.SnapshotKey) < 32 {
			return nil, errors.New("SNAPSHOT_KEY: log management requires the shared runtime secret")
		}
		return logquery.NewKeyring("managed-v1", map[string][]byte{"managed-v1": c.logManagementKey("cursor")})
	}
	if len(c.LogInternalToken) < 32 || len(c.LogInternalToken) > 256 || strings.ContainsAny(c.LogInternalToken, " \r\n\t,") || len(c.LogCursorKeys) > 4096 {
		return nil, errors.New("invalid log query configuration")
	}
	for _, secret := range []string{c.Service.APIKey, c.AuditKey, c.SnapshotKey, c.AdminPassword} {
		if c.LogInternalToken == secret {
			return nil, errors.New("log credential must be independent")
		}
	}
	var encoded map[string]string
	if json.Unmarshal([]byte(c.LogCursorKeys), &encoded) != nil {
		return nil, errors.New("invalid log cursor keyring")
	}
	keys := map[string][]byte{}
	for id, value := range encoded {
		key, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil {
			return nil, errors.New("invalid log cursor key")
		}
		for _, secret := range []string{c.Service.APIKey, c.AuditKey, c.SnapshotKey, c.LogInternalToken} {
			derived := sha256.Sum256([]byte(secret))
			if string(key) == secret || value == secret || bytes.Equal(key, derived[:]) {
				return nil, errors.New("log cursor key must be independent")
			}
		}
		keys[id] = key
	}
	return logquery.NewKeyring(c.LogCursorActive, keys)
}

func (c Runtime) logManagementKey(purpose string) []byte {
	mac := hmac.New(sha256.New, []byte(c.SnapshotKey))
	_, _ = mac.Write([]byte("sicp/log-management/v1/" + purpose))
	return mac.Sum(nil)
}

// LogManagementToken is internal service authentication, never a browser login.
func (c Runtime) LogManagementToken() string {
	if c.LogInternalToken != "" {
		return c.LogInternalToken
	}
	if len(c.SnapshotKey) < 32 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(c.logManagementKey("internal-auth"))
}
