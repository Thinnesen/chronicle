package engine

import (
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

const wfName = "wf"

// sim is a fake cluster. A Pod runs for one step of the engine and then ends with its outcome.
type sim struct {
	t        *testing.T
	in       Input
	outcomes map[string]v1alpha1.NodePhase // by node name; the default is Succeeded
	waves    [][]string                    // node names of the Pods created in each step
	created  map[string]bool
	pods     map[string]CreatePod // by node name
	// outputs are the Pod outputs by node name. The default is the result "out-<display name>".
	outputs map[string]*v1alpha1.Outputs
}

func newSim(t *testing.T, spec string, outcomes map[string]v1alpha1.NodePhase) *sim {
	t.Helper()
	var s v1alpha1.WorkflowTemplateSpec
	if err := yaml.UnmarshalStrict([]byte(spec), &s); err != nil {
		t.Fatalf("bad test spec: %v", err)
	}
	return &sim{
		t:        t,
		in:       Input{Name: wfName, Namespace: "ns", UID: "uid", Spec: &s, Pods: map[string]PodResult{}, Now: at(0)},
		outcomes: outcomes,
		created:  map[string]bool{},
		pods:     map[string]CreatePod{},
		outputs:  map[string]*v1alpha1.Outputs{},
	}
}

func at(step int) metav1.Time {
	return metav1.NewTime(time.Date(2026, 1, 1, 0, 0, step, 0, time.UTC))
}

// step runs the engine once and applies its actions.
func (s *sim) step(i int) Result {
	s.in.Now = at(i)
	for id, p := range s.in.Pods {
		if p.Phase == v1alpha1.NodeRunning {
			p.Phase = s.outcome(id)
			p.FinishedAt = &s.in.Now
			p.Outputs = s.podOutputs(id)
			s.in.Pods[id] = p
		}
	}
	r := Run(s.in)
	wave := make([]string, 0, len(r.Actions))
	for _, a := range r.Actions {
		cp := a.(CreatePod)
		if s.created[cp.NodeID] {
			s.t.Fatalf("Pod of %s created twice", cp.NodeName)
		}
		s.created[cp.NodeID] = true
		s.pods[cp.NodeName] = cp
		wave = append(wave, cp.NodeName)
		s.in.Pods[cp.NodeID] = PodResult{Phase: v1alpha1.NodeRunning, StartedAt: &s.in.Now}
	}
	if len(wave) > 0 {
		s.waves = append(s.waves, wave)
	}
	s.in.Status = r.Status
	return r
}

func (s *sim) outcome(id string) v1alpha1.NodePhase {
	n := s.in.Status.Nodes[id]
	if p, ok := s.outcomes[n.Name]; ok {
		return p
	}
	return v1alpha1.NodeSucceeded
}

func (s *sim) podOutputs(id string) *v1alpha1.Outputs {
	n := s.in.Status.Nodes[id]
	if o, ok := s.outputs[n.Name]; ok {
		return o
	}
	r := "out-" + n.DisplayName
	return &v1alpha1.Outputs{Result: &r}
}

// run steps the engine until the workflow finishes.
func (s *sim) run() v1alpha1.WorkflowStatus {
	for i := range 200 {
		if isFinished(s.step(i).Status.Phase) {
			return s.in.Status
		}
	}
	s.t.Fatalf("the workflow did not finish: %v", phases(s.in.Status))
	return s.in.Status
}

// phases returns the phase of each node by node name.
func phases(st v1alpha1.WorkflowStatus) map[string]v1alpha1.NodePhase {
	out := map[string]v1alpha1.NodePhase{}
	for _, n := range st.Nodes {
		out[n.Name] = n.Phase
	}
	return out
}

func nodeByName(st v1alpha1.WorkflowStatus, name string) v1alpha1.NodeStatus {
	for _, n := range st.Nodes {
		if n.Name == name {
			return n
		}
	}
	return v1alpha1.NodeStatus{}
}

func sortedWaves(w [][]string) [][]string {
	out := make([][]string, len(w))
	for i := range w {
		out[i] = slices.Sorted(slices.Values(w[i]))
	}
	return out
}
