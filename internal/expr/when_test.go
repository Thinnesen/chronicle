package expr

import (
	"slices"
	"testing"
)

func TestWhen(t *testing.T) {
	values := map[string]string{"r": "heads", "n": "10", "t": "true", "inj": "x || true", "q": "it's"}
	resolve := func(n string) (string, bool) { v, ok := values[n]; return v, ok }
	tests := []struct {
		in   string
		want bool
	}{
		{"{{r}} == heads", true},
		{"{{r}} != heads", false},
		{"'{{r}}' == \"heads\"", true},
		{"{{n}} > 9", true}, // numbers compare as numbers
		{"{{n}} > 9.5", true},
		{"{{n}} < 9", false},
		{"{{n}} >= 10 && {{n}} <= 10", true},
		{"abc < abd", true}, // other values compare as text
		{"{{t}}", true},
		{"!{{t}} || {{r}} == tails", false},
		{"({{r}} == tails || {{r}} == heads) && true", true},
		{"{{inj}} == x", false}, // a value is one operand, never code
		{"{{q}} == \"it's\"", true},
		{"false && {{missing}} == x", false}, // short-circuit
		{"true || {{missing}} == x", true},
		{"'pre-{{r}}' == pre-heads", true},
		{"{{r}} =~ hea", true}, // a regular expression match, as in Argo
		{"{{r}} =~ '^tails$'", false},
		{"{{r}} !~ '^t'", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			w, err := ParseWhen(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := w.Eval(resolve)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Eval(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestWhenErrors(t *testing.T) {
	// A constant pattern that is not a valid regular expression fails at once, so that the webhook refuses it.
	for _, in := range []string{"", "a ==", "(a", "a b", "{{x", "{{}}", "'open", "a ~ b", ")", "'{{'", "a =~ '('", "{{x}} !~ '[a'"} {
		if _, err := ParseWhen(in); err == nil {
			t.Errorf("ParseWhen(%q) returned no error", in)
		}
	}
	for _, in := range []string{"heads", "{{missing}}", "{{missing}} == a", "a == {{missing}}", "!x", "x && true", "'{{missing}}' == a", "a =~ '{{missing}}'"} {
		w, err := ParseWhen(in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Eval(func(string) (string, bool) { return "", false }); err == nil {
			t.Errorf("Eval(%q) returned no error", in)
		}
	}
}

func TestWhenPlaceholders(t *testing.T) {
	w, err := ParseWhen("{{a}} == x && !('{{b}}' != y)")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Placeholders(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Placeholders() = %v", got)
	}
}

func FuzzWhen(f *testing.F) {
	f.Add("{{a}} == x && !(b < 3)")
	f.Fuzz(func(t *testing.T, in string) {
		w, err := ParseWhen(in)
		if err != nil {
			return
		}
		w.Placeholders()
		_, _ = w.Eval(func(string) (string, bool) { return "1", true })
	})
}
