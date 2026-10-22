package controller

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/executor"
)

// TestPodExecutorDelivery covers how the main container gets the executor: from an image volume, or from a copy of
// the init container when the image volume is off or the init container must download inputs.
func TestPodExecutorDelivery(t *testing.T) {
	wf := &v1alpha1.Workflow{Name: "wf", Namespace: "default"}
	step := engine.CreatePod{PodName: "wf-1", NodeID: "wf-1", NodeName: "wf.a",
		Template: &v1alpha1.Template{Name: "a", Script: &v1alpha1.ScriptTemplate{Image: "alpine", Source: "true"}}}
	inputs := &executor.ArtifactSpec{Prefix: "p", Inputs: []executor.File{{Name: "data", Path: "/tmp/in"}}}
	outputs := &executor.ArtifactSpec{Prefix: "p", Outputs: []executor.File{{Name: "data", Path: "/tmp/out"}}}
	tests := []struct {
		name        string
		imageVolume bool
		files       *executor.ArtifactSpec
		wantInit    bool
	}{
		{"image volume", true, nil, false},
		{"image volume off", false, nil, true},
		{"outputs only", true, outputs, false},
		{"inputs need the init container", true, inputs, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := buildPod(wf, &v1alpha1.WorkflowTemplateSpec{}, step, testExecutorImage, tt.imageVolume, tt.files)
			main := pod.Spec.Containers[0]
			if got := len(pod.Spec.InitContainers) == 1; got != tt.wantInit {
				t.Fatalf("init container: %v, want %v", got, tt.wantInit)
			}
			if tt.wantInit {
				if main.Command[0] != execPath {
					t.Errorf("command = %v", main.Command)
				}
				return
			}
			if main.Command[0] != imageExecPath {
				t.Errorf("command = %v, want the executor of the image volume", main.Command)
			}
			if !slices.ContainsFunc(pod.Spec.Volumes, func(v corev1.Volume) bool {
				return v.Image != nil && v.Image.Reference == testExecutorImage
			}) {
				t.Errorf("no image volume in %+v", pod.Spec.Volumes)
			}
			if !slices.ContainsFunc(main.VolumeMounts, func(m corev1.VolumeMount) bool {
				return m.Name == imageVolume && m.MountPath == imageDir && m.ReadOnly
			}) {
				t.Errorf("mounts = %+v", main.VolumeMounts)
			}
			if !slices.ContainsFunc(main.Env, func(e corev1.EnvVar) bool { return e.Name == executor.EnvWorkDir && e.Value == execDir }) {
				t.Errorf("env = %+v", main.Env)
			}
		})
	}
}
