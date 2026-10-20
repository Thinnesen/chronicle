package engine

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const cacheSpec = `
entrypoint: main
arguments: {parameters: [{name: day, value: "2026-10-20"}]}
templates:
- name: main
  steps:
  - - {name: report, template: report, arguments: {parameters: [{name: day, value: "{{workflow.parameters.day}}"}]}}
  - - {name: show, template: show, arguments: {parameters: [{name: x, value: "{{steps.report.outputs.result}}"}]}}
- name: report
  inputs: {parameters: [{name: day}]}
  memoize: {key: "report-{{inputs.parameters.day}}", maxAge: 24h}
  container: {image: x, command: [report]}
- name: show
  inputs: {parameters: [{name: x}]}
  container: {image: x, command: [show, "{{inputs.parameters.x}}"]}
`

func TestMemoizeMiss(t *testing.T) {
	s := newSim(t, cacheSpec, nil)
	s.in.CacheEnabled = true
	r := s.step(0)
	if len(r.Actions) != 1 || r.Actions[0] != (LookupCache{Key: "report-2026-10-20"}) || len(s.pods) != 0 {
		t.Fatalf("actions %v, pods %v", r.Actions, s.pods)
	}
	s.in.Cache = map[string]*CacheEntry{"report-2026-10-20": nil}
	saved := 0
	for i := 1; i < 10 && s.in.Status.Phase != v1alpha1.WorkflowSucceeded; i++ {
		for _, a := range s.step(i).Actions {
			if sv, ok := a.(SaveCache); ok && sv.Key == "report-2026-10-20" && *sv.Outputs.Result == "out-report" {
				saved++
			}
		}
	}
	if saved != 1 || s.in.Status.Phase != v1alpha1.WorkflowSucceeded {
		t.Errorf("saved %d times, phase %s", saved, s.in.Status.Phase)
	}
}

func TestMemoizeHit(t *testing.T) {
	result := "1204 rows"
	entry := &CacheEntry{Outputs: &v1alpha1.Outputs{Result: &result}, Workflow: "daily-x1", SavedAt: at(0).Add(-time.Hour)}
	s := newSim(t, cacheSpec, nil)
	s.in.CacheEnabled = true
	s.in.Cache = map[string]*CacheEntry{"report-2026-10-20": entry}
	if st := s.run(); st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase %s: %s", st.Phase, st.Message)
	}
	report := nodeByName(s.in.Status, "wf[0].report")
	if _, ran := s.pods["wf[0].report"]; ran || report.Cache == nil || !report.Cache.Hit || report.Cache.Workflow != "daily-x1" || report.PodName != "" {
		t.Errorf("report ran %v, cache %+v", ran, report.Cache)
	}
	if cmd := s.pods["wf[1].show"].Template.Container.Command; cmd[1] != "1204 rows" {
		t.Errorf("show = %v", cmd)
	}

	// A result older than maxAge does not count.
	entry.SavedAt = metav1.NewTime(at(0).Add(-25 * time.Hour)).Time
	s = newSim(t, cacheSpec, nil)
	s.in.CacheEnabled = true
	s.in.Cache = map[string]*CacheEntry{"report-2026-10-20": entry}
	s.run()
	if _, ran := s.pods["wf[0].report"]; !ran {
		t.Error("an old result was used")
	}
}
