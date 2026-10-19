package convert

import (
	"os"
	"path/filepath"
	"testing"
)

// TestArgoExamplesReport converts the examples of an Argo Workflows clone and prints a summary.
// Run it with ARGO_EXAMPLES=<clone>/examples. It does not fail: it shows what to support next.
func TestArgoExamplesReport(t *testing.T) {
	dir := os.Getenv("ARGO_EXAMPLES")
	if dir == "" {
		t.Skip("set ARGO_EXAMPLES to the examples directory of an Argo Workflows clone")
	}
	// Glob returns the files in lexical order.
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	clean, warned, failed := 0, 0, 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		res, err := Convert(data)
		switch {
		case err != nil:
			failed++
			t.Logf("FAIL %s: %v", filepath.Base(f), err)
		case hasWarnings(res):
			warned++
			for _, r := range res {
				for _, w := range r.Warnings {
					t.Logf("WARN %s: %s", filepath.Base(f), w)
				}
			}
		default:
			clean++
		}
	}
	t.Logf("%d files: %d clean, %d with warnings, %d failed", len(files), clean, warned, failed)
}

func hasWarnings(res []Result) bool {
	for _, r := range res {
		if len(r.Warnings) > 0 {
			return true
		}
	}
	return false
}
