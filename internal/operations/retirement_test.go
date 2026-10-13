package operations

import (
	"strings"
	"testing"
)

func TestT34_04RetirementReferenceInputIsBoundedAndFixed(t *testing.T) {
	for _, raw := range []string{`["task:one","test:two","audit:three"]`, `[]`} {
		if _, err := decodeReferences(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`["path:/etc"]`, `["task:../secret"]`, `["task:a","task:a"]`, `{}`, `["unknown:a"]`, `[null]`, `["task:"]`, strings.Repeat(" ", 160001)} {
		if _, err := decodeReferences(raw); err == nil {
			t.Fatal("invalid reference accepted", raw)
		}
	}
}
