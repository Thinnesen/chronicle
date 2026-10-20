package archive

import (
	"context"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestCache(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	first, second := "1204 rows", "1300 rows"
	if err := s.SaveCache(ctx, "team-a", "report-1", "daily-a", "n1", &v1alpha1.Outputs{Result: &first}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCache(ctx, "team-a", "report-1", "daily-b", "n2", &v1alpha1.Outputs{Result: &second}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCache(ctx, "team-a", []string{"report-1", "report-2"})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got["report-1"]
	if len(got) != 1 || !ok || *e.Outputs.Result != second || e.Workflow != "daily-b" || e.SavedAt.IsZero() {
		t.Errorf("cache = %+v", got)
	}
	if other, _ := s.LoadCache(ctx, "team-b", []string{"report-1"}); len(other) != 0 {
		t.Errorf("another namespace sees %v", other)
	}
}
