package expr

import (
	"testing"
)

func TestParseDependsRefs(t *testing.T) {
	tests := []struct {
		in   string
		want []Ref
	}{
		{"a", []Ref{{Task: "a"}}},
		{"a.Succeeded", []Ref{{Task: "a", Result: ResultSucceeded}}},
		{"a && b.Failed", []Ref{{Task: "a"}, {Task: "b", Result: ResultFailed}}},
		{"(a-1 || b_2.Errored) && !c.Skipped", []Ref{{Task: "a-1"}, {Task: "b_2", Result: ResultErrored}, {Task: "c", Result: ResultSkipped}}},
		{" a.AnySucceeded||b.AllFailed ", []Ref{{Task: "a", Result: ResultAnySucceeded}, {Task: "b", Result: ResultAllFailed}}},
		{"a.Omitted", []Ref{{Task: "a", Result: ResultOmitted}}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			e, err := ParseDepends(tt.in)
			if err != nil {
				t.Fatalf("ParseDepends(%q): %v", tt.in, err)
			}
			got := e.Refs()
			if len(got) != len(tt.want) {
				t.Fatalf("Refs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Refs()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseDependsErrors(t *testing.T) {
	for _, in := range []string{
		"", "   ", "a &&", "&& a", "a b", "(a", "a)", "a.Unknown", "a.", ".Succeeded",
		"a & b", "a | b", "!", "a.Succeeded.Failed", "a + b", "()", "a.Daemoned",
	} {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseDepends(in); err == nil {
				t.Errorf("ParseDepends(%q) returned no error", in)
			}
		})
	}
}

func TestDependsEval(t *testing.T) {
	results := map[string]map[Result]bool{
		"ok":   {ResultSucceeded: true, ResultAnySucceeded: true},
		"bad":  {ResultFailed: true, ResultAllFailed: true},
		"skip": {ResultSkipped: true},
		"err":  {ResultErrored: true},
	}
	lookup := func(task string, r Result) bool { return results[task][r] }
	tests := []struct {
		in   string
		want bool
	}{
		{"ok", true},
		{"skip", true},
		{"bad", false},
		{"err", false},
		{"ok.Succeeded", true},
		{"skip.Succeeded", false},
		{"bad.Failed", true},
		{"err.Errored", true},
		{"ok && bad", false},
		{"ok || bad", true},
		{"!bad", true},
		{"!(ok && bad.Failed)", false},
		{"bad.Failed && (err.Errored || ok.Failed)", true},
		{"ok.AnySucceeded && bad.AllFailed", true},
		{"ok || bad && err", true},
		{"!ok || bad && err", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			e, err := ParseDepends(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := e.Eval(lookup); got != tt.want {
				t.Errorf("Eval(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func FuzzParseDepends(f *testing.F) {
	for _, s := range []string{"a", "a && b.Failed", "!(a||b)", "((a))", "a.AllFailed || !b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		e, err := ParseDepends(in)
		if err != nil {
			return
		}
		e.Refs()
		e.Eval(func(string, Result) bool { return true })
	})
}
