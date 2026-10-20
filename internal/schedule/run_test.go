package schedule

import (
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestBetween(t *testing.T) {
	s, err := Parse([]string{"0 3 * * *"}, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	got, err := s.Between(from, from.Add(72*time.Hour), 10)
	if err != nil || len(got) != 4 || !got[0].Equal(from) || !got[3].Equal(from.Add(72*time.Hour)) {
		t.Fatalf("Between = %v, %v; want 4 times from %s", got, err, from)
	}
	if _, err := s.Between(from, from.Add(72*time.Hour), 3); err == nil {
		t.Error("more times than the limit gave no error")
	}
}

func TestNewRun(t *testing.T) {
	cw := &v1alpha1.CronWorkflow{Name: "nightly", Namespace: "data", UID: "u1"}
	cw.Spec.WorkflowSpec.Entrypoint = "main"
	at := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	wf := NewRun(cw, at)
	if wf.Name != "nightly-1790823600" || wf.Labels[LabelCronWorkflow] != "nightly" || wf.Annotations[AnnotationScheduledTime] != "2026-10-01T03:00:00Z" ||
		wf.Spec.Entrypoint != "main" || len(wf.OwnerReferences) != 1 || wf.OwnerReferences[0].UID != "u1" {
		t.Errorf("NewRun = %+v", wf)
	}
}
