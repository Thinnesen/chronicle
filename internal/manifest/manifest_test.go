package manifest

import (
	"strings"
	"testing"
)

func TestSubstitute(t *testing.T) {
	values := map[string]string{"name": "job-1", "evil": "x\nkind: Secret\n{{oops}}"}
	resolve := func(n string) (string, bool) { v, ok := values[n]; return v, ok }
	got, err := Substitute("kind: ConfigMap\nmetadata: {name: '{{name}}'}\ndata: {a: '{{evil}}', list: [1, '{{name}}']}", resolve)
	if err != nil {
		t.Fatal(err)
	}
	// The injected value stays one string. Its placeholder text is not substituted again.
	want := `{"data":{"a":"x\nkind: Secret\n{{oops}}","list":[1,"job-1"]},"kind":"ConfigMap","metadata":{"name":"job-1"}}`
	if got != want {
		t.Errorf("got %s", got)
	}
	for _, bad := range []string{"{{name}}: x", "- a\n- b", "a: [", "data: {'{{name}}': x}"} {
		if _, err := Substitute(bad, resolve); err == nil {
			t.Errorf("Substitute(%q) returned no error", bad)
		}
	}
	if _, err := Substitute("a: '{{missing}}'", resolve); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("missing placeholder: %v", err)
	}
}
