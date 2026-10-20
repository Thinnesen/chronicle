package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const uid = "0c9e8f2a-3b1d-4e5f-9a8b-7c6d5e4f3a2b"

func TestTraceparent(t *testing.T) {
	tp := Traceparent(uid, "node-1")
	if !strings.HasPrefix(tp, "00-0c9e8f2a3b1d4e5f9a8b7c6d5e4f3a2b-") || !strings.HasSuffix(tp, "-01") || len(tp) != 55 {
		t.Errorf("Traceparent = %q", tp)
	}
	if Traceparent(uid, "node-1") != tp || Traceparent(uid, "node-2") == tp {
		t.Error("the span ID of a node is not stable or not unique")
	}
}

func TestEmitRun(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithIDGenerator(IDGenerator()))
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	run := NodeSpan{ID: "0", Name: "etl-x7", Start: start, End: start.Add(time.Minute), Error: "a step failed",
		Attributes: map[string]string{"chronicle.cron": "", "chronicle.phase": "Failed"}}
	nodes := []NodeSpan{
		{ID: "dag", Name: "etl-x7", Start: start, End: start.Add(time.Minute)},
		{ID: "load", Parent: "dag", Name: "etl-x7.load", Start: start.Add(time.Second), End: start.Add(30 * time.Second)},
	}
	EmitRun(context.Background(), provider.Tracer("test"), uid, run, nodes)
	spans := rec.Ended()
	if len(spans) != 3 {
		t.Fatalf("%d spans", len(spans))
	}
	root, dag, load := spans[0], spans[1], spans[2]
	if root.Parent().IsValid() || !root.StartTime().Equal(start) || root.Status().Description != "a step failed" {
		t.Errorf("root: parent %v, start %v, status %v", root.Parent(), root.StartTime(), root.Status())
	}
	if len(root.Attributes()) != 1 {
		t.Errorf("attributes %v, want no empty values", root.Attributes())
	}
	if dag.Parent().SpanID() != root.SpanContext().SpanID() || load.Parent().SpanID() != dag.SpanContext().SpanID() {
		t.Error("a node span has the wrong parent")
	}
	// The process of the node gets this traceparent, so its spans become children of the node span.
	sc := load.SpanContext()
	if got := "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-01"; got != Traceparent(uid, "load") {
		t.Errorf("node span %s, traceparent %s", got, Traceparent(uid, "load"))
	}
}
