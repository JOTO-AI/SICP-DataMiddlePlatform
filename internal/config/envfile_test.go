package config_test

import (
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
)

func TestT15_14RawEnvironmentPreservesLiteralSecretsAndRejectsAmbiguity(t *testing.T) {
	text := "# fixture\r\nTOKEN=a=$value`literal`#tail\r\nJSON={\"enabled\":true}\n"
	v, e := config.ReadEnv(strings.NewReader(text))
	if e != nil || v["TOKEN"] != "a=$value`literal`#tail" || v["JSON"] != `{"enabled":true}` {
		t.Fatal("raw values transformed", e)
	}
	for _, bad := range []string{"A=one\nA=two\n", "export A=one\n", "A\n", "9A=one\n", "A=bad\x00value\n", "A=" + strings.Repeat("x", 1<<20)} {
		if _, e = config.ReadEnv(strings.NewReader(bad)); e == nil {
			t.Fatal("ambiguous environment accepted")
		}
	}
}
