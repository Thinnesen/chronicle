package controller

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/telemetry"
)

var _ = Describe("Run traces", func() {
	ctx := context.Background()

	It("gives the Pod the traceparent of its node and records the spans when the run finishes", func() {
		rec := tracetest.NewSpanRecorder()
		r := testReconciler()
		r.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithIDGenerator(telemetry.IDGenerator())).Tracer("test")
		key := newWorkflow(ctx, `{entrypoint: main, templates: [{name: main, container: {image: alpine, command: [run]}}]}`)
		wf := reconcileWith(ctx, r, key)
		pods := podsOf(ctx, wf)
		Expect(pods).To(HaveLen(1))
		node := pods[0].Labels[LabelNodeID]
		Expect(pods[0].Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{
			Name: envTraceparent, Value: telemetry.Traceparent(string(wf.UID), "0/"+node),
		}))
		Expect(rec.Ended()).To(BeEmpty())

		finishPod(ctx, &pods[0])
		Eventually(func() chroniclev1alpha1.WorkflowPhase { return reconcileWith(ctx, r, key).Status.Phase }).
			Should(Equal(chroniclev1alpha1.WorkflowSucceeded))
		spans := rec.Ended()
		Expect(spans).To(HaveLen(2))
		Expect(spans[0].Name()).To(Equal(key.Name))
		Expect(spans[1].SpanContext().SpanID()).To(Equal(telemetry.SpanID(string(wf.UID), "0/"+node)))
		Expect(spans[1].Parent().SpanID()).To(Equal(spans[0].SpanContext().SpanID()))
	})
})

func TestRunSpans(t *testing.T) {
	at := func(s int) *metav1.Time { return &metav1.Time{Time: time.Date(2026, 10, 20, 12, 0, s, 0, time.UTC)} }
	wf := &chroniclev1alpha1.Workflow{Name: "etl-x7", Namespace: "data"}
	wf.Status = chroniclev1alpha1.WorkflowStatus{
		Phase: chroniclev1alpha1.WorkflowFailed, Message: "load failed", Restarts: 1, StartedAt: at(10), FinishedAt: at(40),
		Nodes: map[string]chroniclev1alpha1.NodeStatus{
			"dag":  {ID: "dag", Name: "etl-x7", Children: []string{"old", "load", "skip", "fast"}, StartedAt: at(10), FinishedAt: at(40)},
			"old":  {ID: "old", Name: "etl-x7.extract", StartedAt: at(9), FinishedAt: at(10)},
			"fast": {ID: "fast", Name: "etl-x7.check", StartedAt: at(10), FinishedAt: at(10)},
			"load": {ID: "load", Name: "etl-x7.load", Phase: chroniclev1alpha1.NodeFailed, Message: "exit code 1", StartedAt: at(12)},
			"skip": {ID: "skip", Name: "etl-x7.report", Phase: chroniclev1alpha1.NodeOmitted},
		},
	}
	run, nodes := runSpans(wf)
	if run.ID != "1" || run.Error != "load failed" || !run.End.Equal(at(40).Time) {
		t.Errorf("run span %+v", run)
	}
	// The node from the earlier try and the node that never started have no span. A fast node of this try has one.
	if len(nodes) != 3 || nodes[1].ID != "1/fast" {
		t.Fatalf("node spans %+v", nodes)
	}
	load := nodes[2]
	if nodes[0].ID != "1/dag" || nodes[0].Parent != "" || load.ID != "1/load" || load.Parent != "1/dag" {
		t.Errorf("keys %s<-%s, %s<-%s", nodes[0].ID, nodes[0].Parent, load.ID, load.Parent)
	}
	// A node without a finish time ends with the run.
	if load.Error != "exit code 1" || !load.End.Equal(at(40).Time) || load.Name != "etl-x7.load" {
		t.Errorf("load span %+v", load)
	}
}
