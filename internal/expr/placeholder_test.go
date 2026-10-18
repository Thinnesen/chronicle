package expr

import (
	"slices"
	"testing"
)

func TestPlaceholders(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"no placeholders", nil},
		{"{{inputs.parameters.x}}", []string{"inputs.parameters.x"}},
		{"a {{ workflow.name }} b {{tasks.t.outputs.result}}", []string{"workflow.name", "tasks.t.outputs.result"}},
		{"{{a}}{{b}}", []string{"a", "b"}},
		{"single { brace } and }} alone", nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Placeholders(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Placeholders(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPlaceholdersErrors(t *testing.T) {
	for _, in := range []string{"{{", "a {{b", "{{}}", "{{  }}", "{{a{{b}}}}"} {
		t.Run(in, func(t *testing.T) {
			if _, err := Placeholders(in); err == nil {
				t.Errorf("Placeholders(%q) returned no error", in)
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	values := map[string]string{"a": "1", "b.c": "two"}
	got, err := Substitute("x={{a}} y={{ b.c }}", func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "x=1 y=two" {
		t.Errorf("Substitute() = %q", got)
	}
	if _, err := Substitute("{{missing}}", func(string) (string, bool) { return "", false }); err == nil {
		t.Error("Substitute() with an unknown name returned no error")
	}
}
