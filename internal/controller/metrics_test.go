package controller

import (
	"testing"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestRunLabels(t *testing.T) {
	ref := func(name string, cluster bool) *chroniclev1alpha1.Workflow {
		wf := &chroniclev1alpha1.Workflow{Namespace: "data", Labels: map[string]string{LabelCronWorkflow: "nightly"}}
		wf.Spec.WorkflowTemplateRef = &chroniclev1alpha1.WorkflowTemplateRef{Name: name, ClusterScope: cluster}
		return wf
	}
	tests := []struct {
		wf   *chroniclev1alpha1.Workflow
		want [3]string
	}{
		{&chroniclev1alpha1.Workflow{Namespace: "data"}, [3]string{"data", "", ""}},
		{ref("etl", false), [3]string{"data", "etl", "nightly"}},
		{ref("etl", true), [3]string{"data", "cluster/etl", "nightly"}},
	}
	for _, tt := range tests {
		if got := runLabels(tt.wf); got != tt.want {
			t.Errorf("runLabels = %v, want %v", got, tt.want)
		}
	}
}
