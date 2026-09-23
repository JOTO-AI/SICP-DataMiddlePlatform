package transport

import (
	"context"
	"net/http"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
)

type snapshotKey struct{}

func SnapshotToken(ctx context.Context) string {
	token, _ := ctx.Value(snapshotKey{}).(string)
	return token
}

// Reply adds transport metadata without changing the documented business DTO.
type Reply struct {
	Data     any
	Snapshot string
}

func requestSnapshot(header http.Header, op contract.Operation, enabled bool) (string, error) {
	var values []string
	present := false
	for name, entries := range header {
		if strings.EqualFold(name, snapshot.Header) {
			present = true
			values = append(values, entries...)
		}
	}
	if !present {
		return "", nil
	}
	if !enabled || op != contract.StudentGrades && op != contract.BatchGrades {
		return "", &Failure{Category: contract.InvalidArgument}
	}
	if len(values) != 1 || !validSnapshot(values[0]) {
		return "", &Failure{Category: contract.Conflict, Cause: snapshot.ErrSnapshot}
	}
	return values[0], nil
}
func validSnapshot(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
