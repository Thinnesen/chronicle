package engine

import (
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"k8s.io/utils/ptr"
)

func TestEnginePodGC(t *testing.T) {
	spec := dagSpec("    - {name: a, template: c}\n    - {name: b, template: c, depends: a.Succeeded || a.Failed}")
	tests := []struct {
		gc       string
		outcomes map[string]v1alpha1.NodePhase
		deleted  int // Pods deleted at the end
	}{
		{"", nil, 0},
		{"\npodGC: {strategy: OnPodCompletion}", map[string]v1alpha1.NodePhase{"wf.a": failed}, 2},
		{"\npodGC: {strategy: OnPodSuccess}", map[string]v1alpha1.NodePhase{"wf.a": failed}, 1},
		{"\npodGC: {strategy: OnWorkflowSuccess}", nil, 2},
		{"\npodGC: {strategy: OnWorkflowSuccess}", map[string]v1alpha1.NodePhase{"wf.b": failed}, 0},
		{"\npodGC: {strategy: OnWorkflowCompletion, deleteDelayDuration: 30s}", map[string]v1alpha1.NodePhase{"wf.b": failed}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.gc, func(t *testing.T) {
			s := newSim(t, spec+tt.gc, tt.outcomes)
			s.run()
			for i := range 40 { // the steps after the end, for the delay
				s.step(100 + i)
			}
			if len(s.deleted) != tt.deleted {
				t.Errorf("deleted %v", s.deleted)
			}
		})
	}
}

func TestTTL(t *testing.T) {
	end := at(10)
	strategy := &v1alpha1.TTLStrategy{SecondsAfterCompletion: ptr.To[int32](60), SecondsAfterFailure: ptr.To[int32](5)}
	spec := &v1alpha1.WorkflowTemplateSpec{TTLStrategy: strategy}
	tests := []struct {
		phase v1alpha1.WorkflowPhase
		want  time.Duration
		ok    bool
	}{
		{v1alpha1.WorkflowSucceeded, 60 * time.Second, true},
		{v1alpha1.WorkflowFailed, 5 * time.Second, true},
		{v1alpha1.WorkflowRunning, 0, false},
	}
	for _, tt := range tests {
		when, ok := TTL(spec, v1alpha1.WorkflowStatus{Phase: tt.phase, FinishedAt: &end})
		if ok != tt.ok || ok && when.Sub(end.Time) != tt.want {
			t.Errorf("%s: %v %v", tt.phase, when, ok)
		}
	}
	if _, ok := TTL(&v1alpha1.WorkflowTemplateSpec{}, v1alpha1.WorkflowStatus{Phase: v1alpha1.WorkflowSucceeded, FinishedAt: &end}); ok {
		t.Error("a workflow without a TTL got a TTL")
	}
}
