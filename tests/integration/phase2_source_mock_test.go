//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit/querymock"
)

func originalMockQuery(t *testing.T) querymock.Query {
	t.Helper()
	f := newRuntimeFromSource(t, true, 3*time.Minute, nil, true)
	return func(op contract.Operation, body, token string) (map[string]any, string) {
		return f.queryOK(t, op, body, token)
	}
}

func TestI07_OriginalMockStudents(t *testing.T) {
	querymock.Students(t, originalMockQuery(t))
}
