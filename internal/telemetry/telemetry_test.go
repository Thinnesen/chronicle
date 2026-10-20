package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestSetup(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	stop, err := Setup(context.Background(), "test")
	if err != nil || stop(context.Background()) != nil {
		t.Fatalf("without an endpoint: %v", err)
	}
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		t.Fatal("a tracer provider was installed without an endpoint")
	}

	// The exporter connects lazily, so the setup works without a collector.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317")
	stop, err = Setup(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Error("no tracer provider with an endpoint")
	}
	// The provider uses the ID generator of the runs, so a Pod can get its traceparent in advance.
	fixed := context.WithValue(context.Background(), fixedKey{}, fixedIDs{TraceID(uid), SpanID(uid, "0/n")})
	_, span := otel.Tracer("test").Start(fixed, "node")
	if got := span.SpanContext().SpanID(); got != SpanID(uid, "0/n") {
		t.Errorf("span ID %s, want the ID from the run", got)
	}
	span.End()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Do not wait for an export to the missing collector.
	_ = stop(ctx)
}
