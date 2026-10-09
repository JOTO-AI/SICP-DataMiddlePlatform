package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (o Online) SuiteDigest() string {
	if len(o.FixedCases) == 0 {
		return ""
	}
	raw, _ := json.Marshal(o.FixedCases)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func SuiteDigest(q Queries) string {
	if suite, ok := q.(interface{ SuiteDigest() string }); ok {
		return suite.SuiteDigest()
	}
	return ""
}
