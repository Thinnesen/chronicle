package executor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestScriptParts(t *testing.T) {
	for _, src := range []string{"echo hi", strings.Repeat("x", 200<<10), strings.Repeat("é€", 70<<10)} {
		parts := SplitScript(src)
		for i, p := range parts {
			if len(p) > scriptPart || !utf8.ValidString(p) {
				t.Fatalf("part %d of %d has %d bytes or is not valid UTF-8", i, len(parts), len(p))
			}
			t.Setenv(ScriptEnvName(i), p)
		}
		got, ok := readScript()
		if !ok || got != src {
			t.Fatalf("readScript gave %d bytes, want %d", len(got), len(src))
		}
		for _, kv := range childEnv() {
			if strings.HasPrefix(kv, EnvScript) {
				t.Fatalf("the child gets %.30s", kv)
			}
		}
	}
}
