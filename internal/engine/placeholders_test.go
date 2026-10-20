package engine

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// TestRunPlaceholders checks the placeholders of a running node and of an exit handler that Argo workflows use.
func TestRunPlaceholders(t *testing.T) {
	s := newSim(t, `
entrypoint: main
serviceAccountName: runner
onExit: report
templates:
- name: main
  retryStrategy: {limit: 1}
  container: {image: x, command: [echo, "{{retries}}", "{{pod.name}}", "{{node.name}}"]}
- name: report
  container: {image: x, command: [echo, "{{workflow.serviceAccountName}}", "{{workflow.duration}}", "{{workflow.failures}}"]}
`, map[string]v1alpha1.NodePhase{"wf(0)": v1alpha1.NodeFailed, "wf(1)": v1alpha1.NodeFailed})
	if st := s.run(); st.Phase != v1alpha1.WorkflowFailed {
		t.Fatalf("phase %s: %s %v", st.Phase, st.Message, phases(st))
	}
	second := s.pods["wf(1)"]
	if cmd := second.Template.Container.Command; cmd[1] != "1" || cmd[2] != second.PodName || cmd[3] != "wf(1)" {
		t.Errorf("attempt 2: command %v, pod %s", cmd, second.PodName)
	}
	var report []string
	for _, p := range s.pods {
		if c := p.Template.Container.Command; c[1] == "runner" {
			report = c
		}
	}
	if report == nil {
		t.Fatal("the exit handler did not run")
	}
	if d, err := strconv.ParseFloat(report[2], 64); err != nil || d <= 0 {
		t.Errorf("workflow.duration = %q", report[2])
	}
	var failures []map[string]string
	if err := json.Unmarshal([]byte(report[3]), &failures); err != nil || len(failures) != 2 ||
		failures[0]["displayName"] != "wf(0)" || failures[0]["phase"] != "Failed" || failures[0]["podName"] == "" {
		t.Errorf("workflow.failures = %s (%v)", report[3], err)
	}
}

// TestStepStatus checks tasks.x.status, which a later task or an exit handler reads, for example to report a failure.
func TestStepStatus(t *testing.T) {
	s := newSim(t, `
entrypoint: main
templates:
- name: main
  dag:
    tasks:
    - {name: a, template: c}
    - {name: b, template: e, depends: a.Failed, arguments: {parameters: [{name: s, value: "{{tasks.a.status}}"}]}}
- name: c
  container: {image: x}
- name: e
  inputs: {parameters: [{name: s}]}
  container: {image: x, command: [echo, "{{inputs.parameters.s}}"]}
`, map[string]v1alpha1.NodePhase{"wf.a": v1alpha1.NodeFailed})
	s.run()
	if cmd := s.pods["wf.b"].Template.Container.Command; len(cmd) < 2 || cmd[1] != "Failed" {
		t.Errorf("command of b = %v", cmd)
	}
}

// TestDAGOutputArtifact checks that a DAG passes on an artifact of one of its tasks, as in Argo.
func TestDAGOutputArtifact(t *testing.T) {
	s := newSim(t, `
entrypoint: main
templates:
- name: main
  dag:
    tasks:
    - {name: inner, template: inner}
    - {name: use, template: consume, depends: inner, arguments: {artifacts: [{name: in, from: "{{tasks.inner.outputs.artifacts.out}}"}]}}
- name: inner
  outputs: {artifacts: [{name: out, from: "{{tasks.make.outputs.artifacts.data}}"}]}
  dag: {tasks: [{name: make, template: producer}]}
- name: producer
  outputs: {artifacts: [{name: data, path: /tmp/d}]}
  container: {image: x, command: [mk]}
- name: consume
  inputs: {artifacts: [{name: in, path: /tmp/in}]}
  container: {image: x, command: [use]}
`, nil)
	s.outputs["wf.inner.make"] = &v1alpha1.Outputs{Artifacts: []v1alpha1.Artifact{{Name: "data", Key: "ns/uid/make/data.tgz"}}}
	if st := s.run(); st.Phase != v1alpha1.WorkflowSucceeded {
		t.Fatalf("phase %s: %s", st.Phase, st.Message)
	}
	if in := s.pods["wf.use"].Inputs; in == nil || len(in.Artifacts) != 1 || in.Artifacts[0].Key != "ns/uid/make/data.tgz" {
		t.Errorf("inputs of use = %+v", in)
	}
}

// TestWorkflowMetadata checks the placeholders of the labels, the annotations, the parameters and the creation time.
func TestWorkflowMetadata(t *testing.T) {
	s := newSim(t, `
entrypoint: main
arguments: {parameters: [{name: day, value: "2026-10-20"}]}
templates:
- name: main
  container:
    image: x
    command: [echo, "{{workflow.labels.team}}", "{{workflow.labels.json}}", "{{workflow.annotations.example.io/owner}}",
      "{{workflow.annotations.json}}", "{{workflow.parameters.json}}", "{{workflow.mainEntrypoint}}",
      "{{workflow.creationTimestamp.Y}}/{{workflow.creationTimestamp.m}}/{{workflow.creationTimestamp.d}}",
      "{{workflow.creationTimestamp.s}}", "{{workflow.creationTimestamp.RFC3339}}"]
`, nil)
	s.in.Labels = map[string]string{"team": "data"}
	s.in.Annotations = map[string]string{"example.io/owner": "ana"}
	s.in.CreatedAt = metav1.NewTime(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC))
	s.run()
	want := []string{"echo", "data", `{"team":"data"}`, "ana", `{"example.io/owner":"ana"}`,
		`[{"name":"day","value":"2026-10-20"}]`, "main", "2026/03/04", "1772600767", "2026-03-04T05:06:07Z"}
	if cmd := s.pods[wfName].Template.Container.Command; !slices.Equal(cmd, want) {
		t.Errorf("command = %q", cmd)
	}
}
