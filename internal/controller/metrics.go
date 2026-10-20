package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// The metrics of the workflow controller. They are on the metrics endpoint of the controller with the
// metrics of controller-runtime.
var (
	// The template and cron labels name Kubernetes objects, so they have a limited number of values.
	workflowsFinished = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "chronicle_workflows_finished_total",
		Help: "The number of runs that finished, by namespace, WorkflowTemplate, CronWorkflow and phase.",
	}, []string{"namespace", "template", "cron", "phase"})
	workflowDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "chronicle_workflow_duration_seconds",
		Help:    "The time from the start to the end of a run, by namespace, WorkflowTemplate, CronWorkflow and phase.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 16), // 1 s to about 9 h
	}, []string{"namespace", "template", "cron", "phase"})
	podsCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "chronicle_pods_created_total",
		Help: "The number of workflow Pods that the controller created.",
	})
	tasksEnqueued = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "chronicle_tasks_enqueued_total",
		Help: "The number of tasks that the controller put into the task queue, by queue.",
	}, []string{"queue"})
	workflowRetries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "chronicle_workflow_retries_total",
		Help: "The number of retries of finished runs that the controller applied.",
	})
)

func init() {
	metrics.Registry.MustRegister(workflowsFinished, workflowDuration, podsCreated, tasksEnqueued, workflowRetries)
}

// countAction counts the work that an engine action starts.
func countAction(a engine.Action) {
	switch a := a.(type) {
	case engine.CreatePod:
		podsCreated.Inc()
	case engine.EnqueueTask:
		tasksEnqueued.WithLabelValues(a.Queue).Inc()
	}
}

// finishedRun counts a run that has just finished and records an Event on it.
func (r *WorkflowReconciler) finishedRun(wf *chroniclev1alpha1.Workflow) {
	phase := string(wf.Status.Phase)
	l := runLabels(wf)
	workflowsFinished.WithLabelValues(l[0], l[1], l[2], phase).Inc()
	if wf.Status.StartedAt != nil && wf.Status.FinishedAt != nil {
		workflowDuration.WithLabelValues(l[0], l[1], l[2], phase).Observe(wf.Status.FinishedAt.Sub(wf.Status.StartedAt.Time).Seconds())
	}
	if r.Recorder == nil {
		return
	}
	kind := corev1.EventTypeNormal
	if wf.Status.Phase != chroniclev1alpha1.WorkflowSucceeded {
		kind = corev1.EventTypeWarning
	}
	note := "the run " + phase
	if wf.Status.Message != "" {
		note += ": " + wf.Status.Message
	}
	r.Recorder.Eventf(wf, nil, kind, phase, "Finish", "%s", note)
}

// runLabels returns the namespace, the WorkflowTemplate ("cluster/<name>" for a ClusterWorkflowTemplate) and the
// CronWorkflow of a run. The last two are empty when the run has none.
func runLabels(wf *chroniclev1alpha1.Workflow) [3]string {
	template := ""
	if ref := wf.Spec.WorkflowTemplateRef; ref != nil {
		template = ref.Name
		if ref.ClusterScope {
			template = "cluster/" + ref.Name
		}
	}
	return [3]string{wf.Namespace, template, wf.Labels[LabelCronWorkflow]}
}
