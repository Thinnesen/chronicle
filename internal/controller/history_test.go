package controller

import (
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestArchivedOnly(t *testing.T) {
	archived := chroniclev1alpha1.Workflow{Name: "a", Status: chroniclev1alpha1.WorkflowStatus{
		Conditions: []metav1.Condition{{Type: conditionArchived, Status: metav1.ConditionTrue}}}}
	runs := []chroniclev1alpha1.Workflow{archived, {Name: "b"}}
	tests := []struct {
		name    string
		archive bool
		want    []string
	}{
		{"without an archive, all runs over the limit go", false, []string{"a", "b"}},
		{"with an archive, only archived runs go", true, []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := archivedOnly(runs, []string{"a", "b"}, tt.archive); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
