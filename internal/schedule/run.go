package schedule

import (
	"fmt"
	"maps"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

// The label and annotation on Workflows that a CronWorkflow created.
const (
	LabelCronWorkflow       = "chronicle.thinnesen.com/cron-workflow"
	AnnotationScheduledTime = "chronicle.thinnesen.com/scheduled-time"
)

// NewRun returns the run of a CronWorkflow for a schedule time. The name comes from the time, so a second create of
// the same run fails with AlreadyExists.
func NewRun(cw *v1alpha1.CronWorkflow, at time.Time) *v1alpha1.Workflow {
	labels := maps.Clone(cw.Spec.WorkflowMetadata.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[LabelCronWorkflow] = cw.Name
	annotations := maps.Clone(cw.Spec.WorkflowMetadata.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[AnnotationScheduledTime] = at.UTC().Format(time.RFC3339)
	return &v1alpha1.Workflow{
		Name:            cw.Name + "-" + strconv.FormatInt(at.Unix(), 10),
		Namespace:       cw.Namespace,
		Labels:          labels,
		Annotations:     annotations,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cw, v1alpha1.GroupVersion.WithKind("CronWorkflow"))},
		Spec:            *cw.Spec.WorkflowSpec.DeepCopy(),
	}
}

// Between returns the schedule times from from to to, both included. It returns an error when there are more than limit.
func (s *Schedule) Between(from, to time.Time, limit int) ([]time.Time, error) {
	var out []time.Time
	for t := s.Next(from.Add(-time.Second)); !t.After(to); t = s.Next(t) {
		if len(out) == limit {
			return nil, fmt.Errorf("the range has more than %d schedule times: give a shorter range", limit)
		}
		out = append(out, t)
	}
	return out, nil
}
